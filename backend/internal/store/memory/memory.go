// Package memory là bản Store lưu trong bộ nhớ, dùng cho test và chạy thử local.
//
// KHÔNG dùng cho production: không bền, không chịu được nhiều tiến trình. Nó tồn
// tại để logic đồng bộ delta — phần dễ sai nhất của backend — được test kỹ mà
// không cần dựng Postgres.
package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/hauph/camera/backend/internal/protocol"
	"github.com/hauph/camera/backend/internal/store"
)

const defaultLimit = 500

type presetRow struct {
	rec       protocol.Preset
	userID    string
	updatedAt time.Time
	deleted   bool
}

type imageRow struct {
	rec       protocol.ImageRecord
	sessionID string
}

type Store struct {
	mu       sync.Mutex
	newID    store.IDGen
	now      store.Clock
	sessions map[string]*store.Session
	cameras  map[string]*store.Camera
	presets  map[string]*presetRow
	images   map[string]*imageRow
	// byClient khoá theo (sessionID, clientID) — nền tảng của tính idempotent.
	byClient map[string]string
	edits    map[string]*protocol.EditRecord
}

func New(idGen store.IDGen, clock store.Clock) *Store {
	return &Store{
		newID:    idGen,
		now:      clock,
		sessions: map[string]*store.Session{},
		cameras:  map[string]*store.Camera{},
		presets:  map[string]*presetRow{},
		images:   map[string]*imageRow{},
		byClient: map[string]string{},
		edits:    map[string]*protocol.EditRecord{},
	}
}

// looksLikeUUID chỉ kiểm HÌNH DẠNG, không kiểm phiên bản hay checksum: mục đích
// là khớp hành vi của cột uuid trong Postgres, nơi mọi chuỗi sai dạng đều bị từ
// chối trước khi chạm tới khoá ngoại.
func looksLikeUUID(v string) bool {
	if len(v) != 36 {
		return false
	}
	for i, c := range v {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			return false
		}
	}
	return true
}

func clientKey(sessionID, clientID string) string {
	return sessionID + "\x00" + clientID
}

// cloneImage sao chép SÂU bản ghi ảnh trước khi trả ra ngoài.
//
// Copy nông là không đủ và đây là chỗ dễ bỏ sót: `rec := row.rec` sao chép struct
// nhưng `Assets` là map, nên bản sao vẫn trỏ tới CÙNG map với store. Người gọi
// đọc map đó trong khi một goroutine khác chạy ConfirmAsset là tranh chấp dữ
// liệu thật — mutex không bảo vệ được gì sau khi giá trị đã rời khỏi hàm.
func cloneImage(rec protocol.ImageRecord) protocol.ImageRecord {
	if rec.Assets != nil {
		assets := make(map[protocol.AssetTier]protocol.AssetRecord, len(rec.Assets))
		for k, v := range rec.Assets {
			assets[k] = v
		}
		rec.Assets = assets
	}
	return rec
}

// cloneOverrides sao chép map do caller cấp hoặc do store giữ, cùng lý do với
// cloneImage.
func cloneOverrides(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneEdit(rec protocol.EditRecord) protocol.EditRecord {
	rec.Overrides = cloneOverrides(rec.Overrides)
	return rec
}

// nextRevision cấp một revision MỚI cho mỗi bản ghi thay đổi.
//
// Cấp riêng từng bản ghi chứ không dùng chung một revision cho cả lô: nếu nhiều
// bản ghi cùng revision, phân trang sẽ bỏ sót. Xem hợp đồng của Store.Changes.
func (s *Store) nextRevision(sess *store.Session) int64 {
	sess.Revision++
	return sess.Revision
}

func (s *Store) CreateSession(_ context.Context, userID, name, clientName string, startedAt time.Time) (store.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess := &store.Session{
		ID:         s.newID(),
		UserID:     userID,
		Name:       name,
		ClientName: clientName,
		StartedAt:  startedAt,
	}
	s.sessions[sess.ID] = sess
	return *sess, nil
}

func (s *Store) GetSession(_ context.Context, sessionID string) (store.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return store.Session{}, store.ErrNotFound
	}
	return *sess, nil
}

func (s *Store) ListSessions(_ context.Context, userID string, limit int) ([]store.SessionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 || limit > store.MaxSessionList {
		limit = store.MaxSessionList
	}

	counts := map[string]int{}
	for _, row := range s.images {
		if row.rec.Deleted {
			continue
		}
		counts[row.sessionID]++
	}

	out := make([]store.SessionSummary, 0, len(s.sessions))
	for _, sess := range s.sessions {
		if sess.UserID != userID {
			continue
		}
		out = append(out, store.SessionSummary{Session: *sess, ImageCount: counts[sess.ID]})
	}

	// Mới nhất trước, và phá hoà bằng id để thứ tự là TOÀN PHẦN: hai buổi chụp
	// tạo cùng một thời điểm mà đảo chỗ nhau giữa hai lần gọi sẽ khiến danh sách
	// nhấp nháy, và bản pg với bản này sẽ không khớp nhau trong test tuân thủ.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].ID > out[j].ID
	})

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) RegisterCamera(_ context.Context, userID string, in protocol.RegisterCameraRequest) (store.Camera, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, cam := range s.cameras {
		if cam.UserID == userID && cam.Manufacturer == in.Manufacturer && cam.Model == in.Model {
			// Danh tính giữ nguyên, phần đổi theo từng lần kết nối thì cập nhật.
			cam.Firmware = in.Firmware
			cam.Transport = in.Transport
			cam.Capabilities = append([]string(nil), in.Capabilities...)
			cam.LastSeenAt = s.now()
			return *cam, nil
		}
	}

	cam := &store.Camera{
		ID:           s.newID(),
		UserID:       userID,
		Manufacturer: in.Manufacturer,
		Model:        in.Model,
		Firmware:     in.Firmware,
		Transport:    in.Transport,
		Capabilities: append([]string(nil), in.Capabilities...),
		LastSeenAt:   s.now(),
	}
	s.cameras[cam.ID] = cam
	return *cam, nil
}

func (s *Store) GetCamera(_ context.Context, cameraID string) (store.Camera, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cam, ok := s.cameras[cameraID]
	if !ok {
		return store.Camera{}, store.ErrNotFound
	}
	return *cam, nil
}

func (s *Store) CreatePreset(_ context.Context, userID string, p protocol.Preset) (protocol.Preset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Id và version do MÁY CHỦ cấp, không nhận từ client: client tự đặt id nghĩa
	// là nó ghi đè được preset của người khác, và tự đặt version nghĩa là bản
	// đọc sau này không tin được con số đó.
	rec := clonePreset(p)
	rec.ID = s.newID()
	rec.Version = protocol.PresetVersion

	s.presets[rec.ID] = &presetRow{rec: rec, userID: userID, updatedAt: s.now()}
	return clonePreset(rec), nil
}

func (s *Store) ListPresets(_ context.Context, userID string) ([]protocol.Preset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows := []*presetRow{}
	for _, row := range s.presets {
		if row.userID != userID || row.deleted {
			continue
		}
		rows = append(rows, row)
	}
	// Mới nhất trước, phá hoà bằng id để thứ tự là toàn phần — cùng lý do với
	// ListSessions: hai bản triển khai phải xếp giống nhau.
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].updatedAt.Equal(rows[j].updatedAt) {
			return rows[i].updatedAt.After(rows[j].updatedAt)
		}
		return rows[i].rec.ID > rows[j].rec.ID
	})

	out := make([]protocol.Preset, 0, len(rows))
	for _, row := range rows {
		out = append(out, clonePreset(row.rec))
	}
	return out, nil
}

func (s *Store) GetPreset(_ context.Context, presetID string) (protocol.Preset, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row, ok := s.presets[presetID]
	if !ok || row.deleted {
		return protocol.Preset{}, "", store.ErrNotFound
	}
	return clonePreset(row.rec), row.userID, nil
}

func (s *Store) SoftDeletePreset(_ context.Context, presetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	row, ok := s.presets[presetID]
	if !ok || row.deleted {
		return store.ErrNotFound
	}
	row.deleted = true
	return nil
}

// clonePreset sao chép SÂU trước khi trả ra ngoài.
//
// Trả thẳng map bên trong nghĩa là người gọi sửa được dữ liệu của store mà
// không đi qua phương thức nào — cùng lý do với cloneImage ở trên.
func clonePreset(p protocol.Preset) protocol.Preset {
	out := p
	if p.Basic != nil {
		out.Basic = make(map[string]float64, len(p.Basic))
		for k, v := range p.Basic {
			out.Basic[k] = v
		}
	}
	if p.LUT != nil {
		lut := *p.LUT
		out.LUT = &lut
	}
	if p.ToneCurve != nil {
		out.ToneCurve = append([][2]float64(nil), p.ToneCurve...)
	}
	if p.HSL != nil {
		out.HSL = make(map[string]map[string]float64, len(p.HSL))
		for k, m := range p.HSL {
			inner := make(map[string]float64, len(m))
			for ik, iv := range m {
				inner[ik] = iv
			}
			out.HSL[k] = inner
		}
	}
	return out
}

func (s *Store) BatchUpsertImages(_ context.Context, sessionID string, in []protocol.ImageInput) (store.BatchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return store.BatchResult{}, store.ErrNotFound
	}

	now := s.now()
	res := store.BatchResult{IDs: make(map[string]string, len(in))}

	for _, item := range in {
		if item.ClientID == "" {
			return store.BatchResult{}, fmt.Errorf("%w: clientId rỗng", store.ErrConflict)
		}
		if err := s.checkCamera(item.CameraID, sess.UserID); err != nil {
			return store.BatchResult{}, err
		}
		key := clientKey(sessionID, item.ClientID)

		if id, exists := s.byClient[key]; exists {
			row := s.images[id]
			// Chỉ cấp revision mới khi nội dung THẬT SỰ đổi. Nếu không, mỗi lần
			// client retry mù sẽ đẩy revision lên và làm mọi client khác phải
			// đồng bộ lại vô ích — một vòng lặp tự nuôi rất tốn pin và băng thông.
			if sameImage(row.rec, item) {
				res.IDs[item.ClientID] = id
				continue
			}
			applyInput(&row.rec, item)
			row.rec.Revision = s.nextRevision(sess)
			row.rec.UpdatedAt = now
			res.IDs[item.ClientID] = id
			res.Updated++
			continue
		}

		id := s.newID()
		rec := protocol.ImageRecord{ID: id, ClientID: item.ClientID}
		applyInput(&rec, item)
		rec.Revision = s.nextRevision(sess)
		rec.UpdatedAt = now

		s.images[id] = &imageRow{rec: rec, sessionID: sessionID}
		s.byClient[key] = id
		res.IDs[item.ClientID] = id
		res.Created++
	}

	res.Revision = sess.Revision
	return res, nil
}

// checkCamera từ chối ảnh trỏ tới máy ảnh không tồn tại hoặc của người khác.
//
// Gọi phải giữ s.mu.
func (s *Store) checkCamera(cameraID, userID string) error {
	// Rỗng là hợp lệ: ảnh nhập từ nguồn khác, hoặc client cũ chưa đăng ký máy.
	if cameraID == "" {
		return nil
	}
	cam, ok := s.cameras[cameraID]
	if !ok || cam.UserID != userID {
		// Cùng một lỗi cho "không tồn tại" và "của người khác": phân biệt hai
		// trường hợp là cho phép dò id máy ảnh của người lạ.
		return fmt.Errorf("%w: cameraId không thuộc tài khoản này", store.ErrInvalidInput)
	}
	return nil
}

func applyInput(rec *protocol.ImageRecord, in protocol.ImageInput) {
	rec.Filename = in.Filename
	rec.Format = in.Format
	rec.ByteSize = in.ByteSize
	rec.CapturedAt = in.CapturedAt
	rec.IsRaw = in.IsRaw
	rec.CameraID = in.CameraID
}

func sameImage(rec protocol.ImageRecord, in protocol.ImageInput) bool {
	return rec.Filename == in.Filename &&
		rec.Format == in.Format &&
		rec.ByteSize == in.ByteSize &&
		rec.CapturedAt.Equal(in.CapturedAt) &&
		rec.IsRaw == in.IsRaw &&
		rec.CameraID == in.CameraID
}

func (s *Store) Changes(_ context.Context, sessionID string, since int64, limit int) (protocol.ChangesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return protocol.ChangesResponse{}, store.ErrNotFound
	}
	if limit <= 0 || limit > defaultLimit {
		limit = defaultLimit
	}

	// Ảnh và chỉnh sửa dùng CHUNG một bộ đếm revision của session, nên có thể
	// trộn rồi sắp xếp chung. Nếu tách hai con trỏ riêng, client sẽ phải giữ hai
	// trạng thái và rất dễ lệch nhau.
	type entry struct {
		rev   int64
		image *protocol.ImageRecord
		edit  *protocol.EditRecord
	}
	var all []entry

	for _, row := range s.images {
		if row.sessionID != sessionID || row.rec.Revision <= since {
			continue
		}
		rec := cloneImage(row.rec)
		all = append(all, entry{rev: rec.Revision, image: &rec})
	}
	for imageID, ed := range s.edits {
		row, ok := s.images[imageID]
		if !ok || row.sessionID != sessionID || ed.Revision <= since {
			continue
		}
		e := cloneEdit(*ed)
		all = append(all, entry{rev: e.Revision, edit: &e})
	}

	sort.Slice(all, func(i, j int) bool { return all[i].rev < all[j].rev })

	resp := protocol.ChangesResponse{Revision: since}
	if len(all) > limit {
		all = all[:limit]
		resp.HasMore = true
	}
	for _, e := range all {
		if e.image != nil {
			resp.Images = append(resp.Images, *e.image)
		} else {
			resp.Edits = append(resp.Edits, *e.edit)
		}
		resp.Revision = e.rev
	}

	// Không có thay đổi nào: trả về revision hiện tại của session để client
	// nhảy thẳng tới đầu hàng đợi thay vì hỏi lại từ con trỏ cũ.
	if len(all) == 0 {
		resp.Revision = sess.Revision
	}
	return resp, nil
}

func (s *Store) PutEdit(_ context.Context, imageID string, in protocol.PutEditRequest) (protocol.EditRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row, ok := s.images[imageID]
	if !ok {
		return protocol.EditRecord{}, store.ErrNotFound
	}
	// Bản pg có cột uuid nên tự chặn presetId rác; bản này phải chặn tay, nếu
	// không hai bản hành xử khác nhau và mọi test ở tầng trên sẽ xanh trong khi
	// production trả lỗi. Đó đúng là thứ bộ test tuân thủ sinh ra để chặn.
	if in.PresetID != "" && !looksLikeUUID(in.PresetID) {
		return protocol.EditRecord{}, fmt.Errorf("%w: presetId không hợp lệ", store.ErrInvalidInput)
	}
	sess := s.sessions[row.sessionID]

	// Giải quyết xung đột: last-write-wins.
	//
	// Chấp nhận được vì trên thực tế một ảnh gần như luôn được chỉnh trên một
	// thiết bị tại một thời điểm. Nếu sau này xuất hiện khiếu nại "mất chỉnh
	// sửa", UpdatedByDevice là manh mối để chẩn đoán, và khi đó mới cân nhắc
	// khoá lạc quan theo revision.
	rec := protocol.EditRecord{
		ImageID:  imageID,
		PresetID: in.PresetID,
		// Copy map do caller cấp: giữ tham chiếu nghĩa là caller vẫn sửa được
		// dữ liệu đã nằm trong store, sau khi mutex đã nhả.
		Overrides:       cloneOverrides(in.Overrides),
		Rating:          in.Rating,
		Flagged:         in.Flagged,
		Rejected:        in.Rejected,
		Revision:        s.nextRevision(sess),
		UpdatedAt:       s.now(),
		UpdatedByDevice: in.DeviceID,
	}
	s.edits[imageID] = &rec
	return cloneEdit(rec), nil
}

func (s *Store) ConfirmAsset(_ context.Context, imageID string, in protocol.ConfirmAssetRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	row, ok := s.images[imageID]
	if !ok {
		return store.ErrNotFound
	}
	if row.rec.Assets == nil {
		row.rec.Assets = map[protocol.AssetTier]protocol.AssetRecord{}
	}
	row.rec.Assets[in.Tier] = protocol.AssetRecord{
		StorageKey: in.StorageKey,
		ByteSize:   in.ByteSize,
		Width:      in.Width,
		Height:     in.Height,
	}
	// Asset mới là thay đổi client khác cần biết (ảnh đã có bản gốc trên server),
	// nên phải cấp revision mới.
	row.rec.Revision = s.nextRevision(s.sessions[row.sessionID])
	row.rec.UpdatedAt = s.now()
	return nil
}

func (s *Store) GetImage(_ context.Context, imageID string) (protocol.ImageRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row, ok := s.images[imageID]
	if !ok {
		return protocol.ImageRecord{}, store.ErrNotFound
	}
	return cloneImage(row.rec), nil
}

func (s *Store) SessionOfImage(_ context.Context, imageID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row, ok := s.images[imageID]
	if !ok {
		return "", store.ErrNotFound
	}
	return row.sessionID, nil
}

// SoftDeleteImage đánh dấu xoá thay vì xoá hẳn.
//
// Bản ghi phải ở lại để lần đồng bộ sau còn báo được cho các client khác biết mà
// gỡ khỏi danh sách. Xoá hẳn thì client offline sẽ không bao giờ biết.
func (s *Store) SoftDeleteImage(_ context.Context, imageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	row, ok := s.images[imageID]
	if !ok {
		return store.ErrNotFound
	}
	if row.rec.Deleted {
		return nil
	}
	row.rec.Deleted = true
	row.rec.Revision = s.nextRevision(s.sessions[row.sessionID])
	row.rec.UpdatedAt = s.now()
	return nil
}

var _ store.Store = (*Store)(nil)
