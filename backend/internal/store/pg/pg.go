// Package pg là bản triển khai Postgres của các tầng lưu trữ.
//
// Thay thế các bản in-memory. Những bản đó vẫn giữ lại: chúng là nơi logic đồng
// bộ delta và các quy tắc bảo mật được test nhanh mà không cần dựng Postgres,
// còn package này được test bằng Postgres thật để chứng minh SQL đúng.
package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hauph/camera/backend/internal/ids"
	"github.com/hauph/camera/backend/internal/protocol"
	"github.com/hauph/camera/backend/internal/store"
)

const defaultLimit = 500

type Store struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewStore(pool *pgxpool.Pool, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{pool: pool, now: now}
}

func (s *Store) CreateSession(ctx context.Context, userID, name, clientName string, startedAt time.Time) (store.Session, error) {
	sess := store.Session{
		ID: ids.New(), UserID: userID, Name: name,
		ClientName: clientName, StartedAt: startedAt,
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, name, client_name, started_at, revision)
		VALUES ($1, $2, $3, $4, $5, 0)`,
		sess.ID, userID, name, nullIfEmpty(clientName), startedAt)
	if err != nil {
		return store.Session{}, fmt.Errorf("tạo buổi chụp: %w", err)
	}
	return sess, nil
}

func (s *Store) GetSession(ctx context.Context, sessionID string) (store.Session, error) {
	var sess store.Session
	var clientName *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, name, client_name, started_at, revision
		FROM sessions WHERE id = $1`, sessionID).
		Scan(&sess.ID, &sess.UserID, &sess.Name, &clientName, &sess.StartedAt, &sess.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Session{}, store.ErrNotFound
	}
	if err != nil {
		// id không phải UUID hợp lệ cũng rơi vào đây. Với người gọi thì "không
		// tìm thấy" là câu trả lời đúng — và không tiết lộ kiểu dữ liệu của khoá.
		if isInvalidUUID(err) {
			return store.Session{}, store.ErrNotFound
		}
		return store.Session{}, fmt.Errorf("đọc buổi chụp: %w", err)
	}
	if clientName != nil {
		sess.ClientName = *clientName
	}
	return sess, nil
}

func (s *Store) ListSessions(ctx context.Context, userID string, limit int) ([]store.SessionSummary, error) {
	if limit <= 0 || limit > store.MaxSessionList {
		limit = store.MaxSessionList
	}

	// Đếm bằng truy vấn con thay vì LEFT JOIN + GROUP BY: join sẽ nhân bản hàng
	// buổi chụp theo số ảnh rồi gộp lại, và với buổi chụp vài nghìn ảnh thì đó
	// là công vô ích. Truy vấn con chạy trên index (session_id).
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.user_id, s.name, s.client_name, s.started_at, s.revision,
		       (SELECT count(*) FROM images i
		         WHERE i.session_id = s.id AND i.deleted_at IS NULL)
		FROM sessions s
		WHERE s.user_id = $1
		ORDER BY s.started_at DESC, s.id DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("liệt kê buổi chụp: %w", err)
	}
	defer rows.Close()

	out := []store.SessionSummary{}
	for rows.Next() {
		var item store.SessionSummary
		var clientName *string
		if err := rows.Scan(&item.ID, &item.UserID, &item.Name, &clientName,
			&item.StartedAt, &item.Revision, &item.ImageCount); err != nil {
			return nil, fmt.Errorf("đọc buổi chụp: %w", err)
		}
		if clientName != nil {
			item.ClientName = *clientName
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("duyệt buổi chụp: %w", err)
	}
	return out, nil
}

func (s *Store) RegisterCamera(ctx context.Context, userID string, in protocol.RegisterCameraRequest) (store.Camera, error) {
	cam := store.Camera{
		ID: ids.New(), UserID: userID,
		Manufacturer: in.Manufacturer, Model: in.Model, Firmware: in.Firmware,
		Transport: in.Transport, Capabilities: in.Capabilities, LastSeenAt: s.now(),
	}
	if cam.Capabilities == nil {
		cam.Capabilities = []string{}
	}

	// ON CONFLICT theo (user_id, manufacturer, model): cắm lại cùng một thân máy
	// KHÔNG được sinh bản ghi mới, nếu không mỗi lần mở app lại đẻ thêm một
	// "máy ảnh" và bảng này thành rác sau một tháng.
	//
	// RETURNING trả về dòng đã có khi trùng, nên người gọi luôn nhận đúng id
	// đang dùng thật — kể cả khi bản ghi được tạo từ lần kết nối trước.
	var firmware *string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO cameras (id, user_id, manufacturer, model, firmware, transport,
			capabilities, last_seen_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (user_id, manufacturer, model) DO UPDATE
			SET firmware = EXCLUDED.firmware,
			    transport = EXCLUDED.transport,
			    capabilities = EXCLUDED.capabilities,
			    last_seen_at = EXCLUDED.last_seen_at
		RETURNING id, firmware, last_seen_at`,
		cam.ID, userID, in.Manufacturer, in.Model, nullIfEmpty(in.Firmware),
		in.Transport, cam.Capabilities, cam.LastSeenAt).
		Scan(&cam.ID, &firmware, &cam.LastSeenAt)
	if err != nil {
		return store.Camera{}, fmt.Errorf("đăng ký máy ảnh: %w", err)
	}
	if firmware != nil {
		cam.Firmware = *firmware
	}
	return cam, nil
}

func (s *Store) GetCamera(ctx context.Context, cameraID string) (store.Camera, error) {
	var cam store.Camera
	var firmware *string
	var lastSeen *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, manufacturer, model, firmware, transport, capabilities, last_seen_at
		FROM cameras WHERE id = $1`, cameraID).
		Scan(&cam.ID, &cam.UserID, &cam.Manufacturer, &cam.Model, &firmware,
			&cam.Transport, &cam.Capabilities, &lastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Camera{}, store.ErrNotFound
	}
	if err != nil {
		if isInvalidUUID(err) {
			return store.Camera{}, store.ErrNotFound
		}
		return store.Camera{}, fmt.Errorf("đọc máy ảnh: %w", err)
	}
	if firmware != nil {
		cam.Firmware = *firmware
	}
	if lastSeen != nil {
		cam.LastSeenAt = *lastSeen
	}
	return cam, nil
}

func (s *Store) CreatePreset(ctx context.Context, userID string, p protocol.Preset) (protocol.Preset, error) {
	// Id và version do MÁY CHỦ cấp, không nhận từ client: client tự đặt id nghĩa
	// là nó ghi đè được preset của người khác.
	p.ID = ids.New()
	p.Version = protocol.PresetVersion

	// `body` giữ NGUYÊN tài liệu preset, kể cả những khoá mà bản hiện tại chưa
	// dùng tới. `name` và `version` chỉ là bản sao ra cột để query được — xem
	// chú thích trong migrations/0001_init.sql.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO presets (id, user_id, name, version, body, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$6)`,
		p.ID, userID, p.Name, p.Version, p, s.now()); err != nil {
		return protocol.Preset{}, fmt.Errorf("tạo preset: %w", err)
	}
	return p, nil
}

func (s *Store) ListPresets(ctx context.Context, userID string) ([]protocol.Preset, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT body FROM presets
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY updated_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("liệt kê preset: %w", err)
	}
	defer rows.Close()

	out := []protocol.Preset{}
	for rows.Next() {
		var p protocol.Preset
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("đọc preset: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetPreset(ctx context.Context, presetID string) (protocol.Preset, string, error) {
	var p protocol.Preset
	var ownerID string
	err := s.pool.QueryRow(ctx, `
		SELECT body, user_id FROM presets
		WHERE id = $1 AND deleted_at IS NULL`, presetID).Scan(&p, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return protocol.Preset{}, "", store.ErrNotFound
	}
	if err != nil {
		if isInvalidUUID(err) {
			return protocol.Preset{}, "", store.ErrNotFound
		}
		return protocol.Preset{}, "", fmt.Errorf("đọc preset: %w", err)
	}
	return p, ownerID, nil
}

func (s *Store) SoftDeletePreset(ctx context.Context, presetID string) error {
	// Xoá MỀM: ảnh đã chỉnh vẫn trỏ tới preset này qua khoá ngoại, và xoá cứng
	// sẽ làm mất dấu vết "tấm này dùng look nào" của những buổi chụp đã giao.
	tag, err := s.pool.Exec(ctx, `
		UPDATE presets SET deleted_at = $2, updated_at = $2
		WHERE id = $1 AND deleted_at IS NULL`, presetID, s.now())
	if err != nil {
		if isInvalidUUID(err) {
			return store.ErrNotFound
		}
		return fmt.Errorf("xoá preset: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

// allocRevisions cấp một DẢI revision liên tiếp trong một lần cập nhật.
//
// Mỗi bản ghi thay đổi cần một revision RIÊNG BIỆT — dùng chung một revision cho
// cả lô sẽ khiến phân trang bỏ sót bản ghi (xem hợp đồng của store.Changes). Cách
// ngây thơ là cập nhật sessions.revision một lần cho mỗi dòng, nhưng với lô 500
// ảnh đó là 500 lần ghi vào cùng một dòng, tuần tự hoá toàn bộ.
//
// Cấp cả dải một lần: revision cuối trả về là `last`, và dải là
// [last-n+1 .. last].
func allocRevisions(ctx context.Context, tx pgx.Tx, sessionID string, n int) (int64, error) {
	if n <= 0 {
		return 0, nil
	}
	var last int64
	err := tx.QueryRow(ctx, `
		UPDATE sessions SET revision = revision + $2, updated_at = now()
		WHERE id = $1 RETURNING revision`, sessionID, n).Scan(&last)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, store.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("cấp revision: %w", err)
	}
	return last, nil
}

func (s *Store) BatchUpsertImages(ctx context.Context, sessionID string, in []protocol.ImageInput) (store.BatchResult, error) {
	res := store.BatchResult{IDs: make(map[string]string, len(in))}

	err := s.inTx(ctx, func(tx pgx.Tx) error {
		// Khoá dòng buổi chụp ngay từ đầu. Hai client cùng đẩy vào một buổi chụp
		// là chuyện bình thường (điện thoại và iPad), và nếu không tuần tự hoá,
		// hai giao dịch có thể cùng đọc trạng thái cũ rồi cùng cấp revision chồng
		// lấn nhau.
		var current int64
		var ownerID string
		err := tx.QueryRow(ctx,
			`SELECT revision, user_id FROM sessions WHERE id = $1 FOR UPDATE`, sessionID).
			Scan(&current, &ownerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		if err != nil {
			if isInvalidUUID(err) {
				return store.ErrNotFound
			}
			return err
		}

		clientIDs := make([]string, 0, len(in))
		for _, item := range in {
			if item.ClientID == "" {
				return fmt.Errorf("%w: clientId rỗng", store.ErrConflict)
			}
			clientIDs = append(clientIDs, item.ClientID)
		}

		if err := checkCameras(ctx, tx, in, ownerID); err != nil {
			return err
		}

		existing, err := existingImages(ctx, tx, sessionID, clientIDs)
		if err != nil {
			return err
		}

		// Phân loại TRƯỚC khi cấp revision, để retry mù (gửi lại lô y hệt) không
		// đẩy revision lên. Nếu có, mọi client khác phải đồng bộ lại vô ích và
		// tạo một vòng lặp tự nuôi tốn pin lẫn băng thông.
		type pending struct {
			item  protocol.ImageInput
			id    string
			isNew bool
		}
		var changed []pending
		for _, item := range in {
			row, ok := existing[item.ClientID]
			if !ok {
				changed = append(changed, pending{item: item, id: ids.New(), isNew: true})
				continue
			}
			res.IDs[item.ClientID] = row.id
			if !sameImage(row, item) {
				changed = append(changed, pending{item: item, id: row.id})
			}
		}

		last, err := allocRevisions(ctx, tx, sessionID, len(changed))
		if err != nil {
			return err
		}
		rev := last - int64(len(changed))

		now := s.now()
		batch := &pgx.Batch{}
		for _, p := range changed {
			rev++
			res.IDs[p.item.ClientID] = p.id
			if p.isNew {
				res.Created++
				batch.Queue(`
					INSERT INTO images (id, session_id, client_id, filename, format,
						byte_size, captured_at, is_raw, camera_id, revision, created_at, updated_at)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)`,
					p.id, sessionID, p.item.ClientID, p.item.Filename, string(p.item.Format),
					p.item.ByteSize, p.item.CapturedAt, p.item.IsRaw,
					nullIfEmpty(p.item.CameraID), rev, now)
			} else {
				res.Updated++
				batch.Queue(`
					UPDATE images SET filename=$2, format=$3, byte_size=$4,
						captured_at=$5, is_raw=$6, camera_id=$7, revision=$8, updated_at=$9
					WHERE id = $1`,
					p.id, p.item.Filename, string(p.item.Format), p.item.ByteSize,
					p.item.CapturedAt, p.item.IsRaw, nullIfEmpty(p.item.CameraID), rev, now)
			}
		}
		if batch.Len() > 0 {
			br := tx.SendBatch(ctx, batch)
			if err := br.Close(); err != nil {
				return fmt.Errorf("ghi lô ảnh: %w", err)
			}
		}

		if len(changed) == 0 {
			res.Revision = current
		} else {
			res.Revision = last
		}
		return nil
	})
	if err != nil {
		return store.BatchResult{}, err
	}
	return res, nil
}

type imageRow struct {
	id         string
	filename   string
	format     string
	byteSize   int64
	capturedAt time.Time
	isRaw      bool
	cameraID   string
}

func existingImages(ctx context.Context, tx pgx.Tx, sessionID string, clientIDs []string) (map[string]imageRow, error) {
	rows, err := tx.Query(ctx, `
		SELECT client_id, id, filename, format, byte_size, captured_at, is_raw, camera_id
		FROM images WHERE session_id = $1 AND client_id = ANY($2)`, sessionID, clientIDs)
	if err != nil {
		return nil, fmt.Errorf("đọc ảnh có sẵn: %w", err)
	}
	defer rows.Close()

	out := make(map[string]imageRow, len(clientIDs))
	for rows.Next() {
		var cid string
		var r imageRow
		var cameraID *string
		if err := rows.Scan(&cid, &r.id, &r.filename, &r.format, &r.byteSize,
			&r.capturedAt, &r.isRaw, &cameraID); err != nil {
			return nil, err
		}
		if cameraID != nil {
			r.cameraID = *cameraID
		}
		out[cid] = r
	}
	return out, rows.Err()
}

// checkCameras từ chối cả lô nếu có ảnh trỏ tới máy ảnh không thuộc chủ buổi chụp.
//
// Kiểm bằng MỘT truy vấn cho cả lô: một lô là 200 ảnh và gần như luôn cùng một
// máy ảnh, nên hỏi từng ảnh là 200 vòng vô ích.
func checkCameras(ctx context.Context, tx pgx.Tx, in []protocol.ImageInput, ownerID string) error {
	seen := map[string]struct{}{}
	ids := []string{}
	for _, item := range in {
		// Rỗng là hợp lệ: ảnh nhập từ nguồn khác, hoặc client cũ chưa đăng ký máy.
		if item.CameraID == "" {
			continue
		}
		if _, ok := seen[item.CameraID]; ok {
			continue
		}
		seen[item.CameraID] = struct{}{}
		ids = append(ids, item.CameraID)
	}
	if len(ids) == 0 {
		return nil
	}

	var found int
	err := tx.QueryRow(ctx,
		`SELECT count(*) FROM cameras WHERE id = ANY($1) AND user_id = $2`, ids, ownerID).
		Scan(&found)
	if err != nil {
		// id không phải UUID cũng rơi vào đây, và với người gọi thì đó cũng là
		// "cameraId không hợp lệ" — không cần phân biệt.
		if isInvalidUUID(err) {
			return fmt.Errorf("%w: cameraId không hợp lệ", store.ErrInvalidInput)
		}
		return fmt.Errorf("kiểm máy ảnh: %w", err)
	}
	if found != len(ids) {
		// Cùng một lỗi cho "không tồn tại" và "của người khác": phân biệt là cho
		// phép dò id máy ảnh của người lạ.
		return fmt.Errorf("%w: cameraId không thuộc tài khoản này", store.ErrInvalidInput)
	}
	return nil
}

func sameImage(r imageRow, in protocol.ImageInput) bool {
	return r.filename == in.Filename &&
		r.format == string(in.Format) &&
		r.byteSize == in.ByteSize &&
		// So sánh bằng Equal chứ không phải ==: Postgres trả về múi giờ của phiên,
		// nên hai time.Time cùng thời điểm có thể khác Location và == sẽ sai.
		r.capturedAt.Equal(in.CapturedAt) &&
		r.isRaw == in.IsRaw &&
		r.cameraID == in.CameraID
}

func (s *Store) Changes(ctx context.Context, sessionID string, since int64, limit int) (protocol.ChangesResponse, error) {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return protocol.ChangesResponse{}, err
	}
	if limit <= 0 || limit > defaultLimit {
		limit = defaultLimit
	}

	// Trộn ảnh và chỉnh sửa trong MỘT truy vấn rồi sắp xếp chung theo revision.
	//
	// Cả hai dùng chung bộ đếm revision của buổi chụp, nên trộn được. Tách hai
	// con trỏ riêng sẽ buộc client giữ hai trạng thái và rất dễ lệch nhau.
	//
	// LIMIT áp lên tập đã trộn, nên phân trang không bao giờ cắt ngang giữa hai
	// loại — mỗi bản ghi có revision riêng biệt nên thứ tự là toàn phần.
	rows, err := s.pool.Query(ctx, `
		WITH merged AS (
			SELECT i.revision, 'image' AS kind, i.id AS image_id, i.client_id,
			       i.filename, i.format, i.byte_size, i.captured_at, i.is_raw,
			       i.camera_id,
			       i.deleted_at IS NOT NULL AS deleted, i.updated_at,
			       NULL::uuid AS preset_id, NULL::jsonb AS overrides,
			       NULL::smallint AS rating, NULL::boolean AS flagged,
			       NULL::boolean AS rejected, NULL::text AS device
			FROM images i
			WHERE i.session_id = $1 AND i.revision > $2

			UNION ALL

			SELECT e.revision, 'edit', e.image_id, NULL, NULL, NULL, NULL, NULL, NULL,
			       NULL::uuid,
			       false, e.updated_at,
			       e.preset_id, e.overrides, e.rating, e.flagged, e.rejected,
			       e.updated_by_device
			FROM image_edits e
			JOIN images i2 ON i2.id = e.image_id
			WHERE i2.session_id = $1 AND e.revision > $2
		)
		SELECT * FROM merged ORDER BY revision LIMIT $3`,
		sessionID, since, limit+1)
	if err != nil {
		return protocol.ChangesResponse{}, fmt.Errorf("đọc thay đổi: %w", err)
	}
	defer rows.Close()

	resp := protocol.ChangesResponse{
		Images:   []protocol.ImageRecord{},
		Edits:    []protocol.EditRecord{},
		Revision: since,
	}

	var scanned int
	for rows.Next() {
		scanned++
		// Lấy limit+1 dòng để biết CÒN NỮA hay không mà không phải chạy thêm một
		// truy vấn đếm. Dòng thừa không được đưa vào kết quả.
		if scanned > limit {
			resp.HasMore = true
			break
		}

		var (
			rev                        int64
			kind                       string
			imageID                    string
			clientID, filename, format *string
			byteSize                   *int64
			capturedAt                 *time.Time
			isRaw                      *bool
			cameraID                   *string
			deleted                    bool
			updatedAt                  time.Time
			presetID                   *string
			overrides                  map[string]any
			rating                     *int16
			flagged, rejected          *bool
			device                     *string
		)
		if err := rows.Scan(&rev, &kind, &imageID, &clientID, &filename, &format,
			&byteSize, &capturedAt, &isRaw, &cameraID, &deleted, &updatedAt,
			&presetID, &overrides, &rating, &flagged, &rejected, &device); err != nil {
			return protocol.ChangesResponse{}, fmt.Errorf("đọc dòng thay đổi: %w", err)
		}

		if kind == "image" {
			rec := protocol.ImageRecord{
				ID: imageID, Revision: rev, Deleted: deleted, UpdatedAt: updatedAt,
			}
			if clientID != nil {
				rec.ClientID = *clientID
			}
			if filename != nil {
				rec.Filename = *filename
			}
			if format != nil {
				rec.Format = protocol.ImageFormat(*format)
			}
			if cameraID != nil {
				rec.CameraID = *cameraID
			}
			if byteSize != nil {
				rec.ByteSize = *byteSize
			}
			if capturedAt != nil {
				rec.CapturedAt = *capturedAt
			}
			if isRaw != nil {
				rec.IsRaw = *isRaw
			}
			resp.Images = append(resp.Images, rec)
		} else {
			rec := protocol.EditRecord{
				ImageID: imageID, Revision: rev, UpdatedAt: updatedAt, Overrides: overrides,
			}
			if presetID != nil {
				rec.PresetID = *presetID
			}
			if rating != nil {
				rec.Rating = int(*rating)
			}
			if flagged != nil {
				rec.Flagged = *flagged
			}
			if rejected != nil {
				rec.Rejected = *rejected
			}
			if device != nil {
				rec.UpdatedByDevice = *device
			}
			resp.Edits = append(resp.Edits, rec)
		}
		resp.Revision = rev
	}
	if err := rows.Err(); err != nil {
		return protocol.ChangesResponse{}, err
	}

	// Không có thay đổi nào: trả revision hiện tại của buổi chụp để client nhảy
	// thẳng tới đầu hàng đợi thay vì hỏi lại từ con trỏ cũ.
	if len(resp.Images) == 0 && len(resp.Edits) == 0 {
		resp.Revision = sess.Revision
	}

	// Nạp asset cho các ảnh trả về. Truy vấn riêng thay vì JOIN: một ảnh có nhiều
	// asset, và JOIN sẽ nhân dòng lên khiến LIMIT ở trên đếm sai.
	if err := s.attachAssets(ctx, resp.Images); err != nil {
		return protocol.ChangesResponse{}, err
	}
	return resp, nil
}

func (s *Store) attachAssets(ctx context.Context, imgs []protocol.ImageRecord) error {
	if len(imgs) == 0 {
		return nil
	}
	idList := make([]string, len(imgs))
	for i, im := range imgs {
		idList[i] = im.ID
	}

	rows, err := s.pool.Query(ctx, `
		SELECT image_id, tier, storage_key, byte_size, width, height
		FROM image_assets WHERE image_id = ANY($1)`, idList)
	if err != nil {
		return fmt.Errorf("đọc asset: %w", err)
	}
	defer rows.Close()

	byImage := map[string]map[protocol.AssetTier]protocol.AssetRecord{}
	for rows.Next() {
		var imgID, tier, key string
		var size int64
		var w, h *int32
		if err := rows.Scan(&imgID, &tier, &key, &size, &w, &h); err != nil {
			return err
		}
		if byImage[imgID] == nil {
			byImage[imgID] = map[protocol.AssetTier]protocol.AssetRecord{}
		}
		rec := protocol.AssetRecord{StorageKey: key, ByteSize: size}
		if w != nil {
			rec.Width = int(*w)
		}
		if h != nil {
			rec.Height = int(*h)
		}
		byImage[imgID][protocol.AssetTier(tier)] = rec
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range imgs {
		if a, ok := byImage[imgs[i].ID]; ok {
			imgs[i].Assets = a
		}
	}
	return nil
}

func (s *Store) PutEdit(ctx context.Context, imageID string, in protocol.PutEditRequest) (protocol.EditRecord, error) {
	var rec protocol.EditRecord

	err := s.inTx(ctx, func(tx pgx.Tx) error {
		sessionID, err := sessionOfImageTx(ctx, tx, imageID)
		if err != nil {
			return err
		}
		last, err := allocRevisions(ctx, tx, sessionID, 1)
		if err != nil {
			return err
		}

		now := s.now()
		overrides := in.Overrides
		if overrides == nil {
			overrides = map[string]any{}
		}

		// Giải quyết xung đột: last-write-wins. Chấp nhận được vì trên thực tế một
		// ảnh gần như luôn được chỉnh trên một thiết bị tại một thời điểm.
		// updated_by_device là manh mối để chẩn đoán nếu có khiếu nại mất chỉnh sửa.
		_, err = tx.Exec(ctx, `
			INSERT INTO image_edits (image_id, preset_id, overrides, rating, flagged,
				rejected, revision, updated_at, updated_by_device)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (image_id) DO UPDATE SET
				preset_id = EXCLUDED.preset_id,
				overrides = EXCLUDED.overrides,
				rating = EXCLUDED.rating,
				flagged = EXCLUDED.flagged,
				rejected = EXCLUDED.rejected,
				revision = EXCLUDED.revision,
				updated_at = EXCLUDED.updated_at,
				updated_by_device = EXCLUDED.updated_by_device`,
			imageID, nullIfEmptyUUID(in.PresetID), overrides, in.Rating,
			in.Flagged, in.Rejected, last, now, nullIfEmpty(in.DeviceID))
		if err != nil {
			// presetId rác (không phải UUID) hoặc trỏ tới preset không tồn tại là
			// LỖI CỦA CLIENT, không phải sự cố máy chủ. Không phân loại ở đây thì
			// nó thành 500 "lỗi máy chủ" và người viết client đi tìm nhầm chỗ —
			// đúng cái đã xảy ra khi app gửi id preset dựng sẵn.
			if isInvalidUUID(err) || isForeignKeyViolation(err) {
				return fmt.Errorf("%w: presetId không hợp lệ", store.ErrInvalidInput)
			}
			return fmt.Errorf("ghi chỉnh sửa: %w", err)
		}

		rec = protocol.EditRecord{
			ImageID: imageID, PresetID: in.PresetID, Overrides: in.Overrides,
			Rating: in.Rating, Flagged: in.Flagged, Rejected: in.Rejected,
			Revision: last, UpdatedAt: now, UpdatedByDevice: in.DeviceID,
		}
		return nil
	})
	return rec, err
}

func (s *Store) ConfirmAsset(ctx context.Context, imageID string, in protocol.ConfirmAssetRequest) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		sessionID, err := sessionOfImageTx(ctx, tx, imageID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO image_assets (image_id, tier, storage_key, byte_size, width, height)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (image_id, tier) DO UPDATE SET
				storage_key = EXCLUDED.storage_key,
				byte_size = EXCLUDED.byte_size,
				width = EXCLUDED.width,
				height = EXCLUDED.height`,
			imageID, string(in.Tier), in.StorageKey, in.ByteSize,
			nullIfZero(in.Width), nullIfZero(in.Height))
		if err != nil {
			return fmt.Errorf("ghi asset: %w", err)
		}

		// Asset mới là thay đổi các client khác cần biết (ảnh đã có bản gốc trên
		// server), nên phải cấp revision mới cho ảnh.
		last, err := allocRevisions(ctx, tx, sessionID, 1)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE images SET revision = $2, updated_at = $3 WHERE id = $1`,
			imageID, last, s.now())
		return err
	})
}

func (s *Store) GetImage(ctx context.Context, imageID string) (protocol.ImageRecord, error) {
	var rec protocol.ImageRecord
	var deletedAt *time.Time
	var cameraID *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, client_id, filename, format, byte_size, captured_at, is_raw,
		       camera_id, revision, updated_at, deleted_at
		FROM images WHERE id = $1`, imageID).
		Scan(&rec.ID, &rec.ClientID, &rec.Filename, &rec.Format, &rec.ByteSize,
			&rec.CapturedAt, &rec.IsRaw, &cameraID, &rec.Revision, &rec.UpdatedAt, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return protocol.ImageRecord{}, store.ErrNotFound
	}
	if err != nil {
		if isInvalidUUID(err) {
			return protocol.ImageRecord{}, store.ErrNotFound
		}
		return protocol.ImageRecord{}, fmt.Errorf("đọc ảnh: %w", err)
	}
	rec.Deleted = deletedAt != nil
	if cameraID != nil {
		rec.CameraID = *cameraID
	}

	imgs := []protocol.ImageRecord{rec}
	if err := s.attachAssets(ctx, imgs); err != nil {
		return protocol.ImageRecord{}, err
	}
	return imgs[0], nil
}

func (s *Store) SessionOfImage(ctx context.Context, imageID string) (string, error) {
	var sessionID string
	err := s.pool.QueryRow(ctx, `SELECT session_id FROM images WHERE id = $1`, imageID).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", store.ErrNotFound
	}
	if err != nil {
		if isInvalidUUID(err) {
			return "", store.ErrNotFound
		}
		return "", err
	}
	return sessionID, nil
}

func sessionOfImageTx(ctx context.Context, tx pgx.Tx, imageID string) (string, error) {
	var sessionID string
	err := tx.QueryRow(ctx, `SELECT session_id FROM images WHERE id = $1`, imageID).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", store.ErrNotFound
	}
	if err != nil {
		if isInvalidUUID(err) {
			return "", store.ErrNotFound
		}
		return "", err
	}
	return sessionID, nil
}

// SoftDeleteImage đánh dấu xoá thay vì xoá hẳn.
//
// Bản ghi phải ở lại để lần đồng bộ sau còn báo được cho các client khác mà gỡ
// khỏi danh sách. Xoá hẳn thì client offline sẽ không bao giờ biết.
func (s *Store) SoftDeleteImage(ctx context.Context, imageID string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		var sessionID string
		var deletedAt *time.Time
		err := tx.QueryRow(ctx, `SELECT session_id, deleted_at FROM images WHERE id = $1`, imageID).
			Scan(&sessionID, &deletedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		if err != nil {
			if isInvalidUUID(err) {
				return store.ErrNotFound
			}
			return err
		}
		if deletedAt != nil {
			return nil // đã xoá rồi, không cấp revision mới
		}

		last, err := allocRevisions(ctx, tx, sessionID, 1)
		if err != nil {
			return err
		}
		now := s.now()
		_, err = tx.Exec(ctx,
			`UPDATE images SET deleted_at = $2, revision = $3, updated_at = $2 WHERE id = $1`,
			imageID, now, last)
		return err
	})
}

func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return err
	}
	return tx.Commit(ctx)
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nullIfEmptyUUID cho cột uuid: chuỗi rỗng không phải uuid hợp lệ, phải là NULL.
func nullIfEmptyUUID(s string) *string {
	return nullIfEmpty(s)
}

func nullIfZero(v int) *int32 {
	if v == 0 {
		return nil
	}
	x := int32(v)
	return &x
}

// isInvalidUUID nhận diện lỗi ép kiểu của Postgres khi id không đúng định dạng.
//
// Người gọi truyền id lấy từ URL, và một id rác phải cho ra "không tìm thấy" chứ
// không phải lỗi 500 — vừa đúng về ngữ nghĩa, vừa không tiết lộ kiểu khoá.
func isForeignKeyViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		// 23503 = foreign_key_violation
		return pgErr.SQLState() == "23503"
	}
	return false
}

func isInvalidUUID(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		// 22P02 = invalid_text_representation
		return pgErr.SQLState() == "22P02"
	}
	return false
}

var _ store.Store = (*Store)(nil)
