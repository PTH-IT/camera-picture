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

type imageRow struct {
	rec       protocol.ImageRecord
	sessionID string
}

type Store struct {
	mu       sync.Mutex
	newID    store.IDGen
	now      store.Clock
	sessions map[string]*store.Session
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
		images:   map[string]*imageRow{},
		byClient: map[string]string{},
		edits:    map[string]*protocol.EditRecord{},
	}
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

func applyInput(rec *protocol.ImageRecord, in protocol.ImageInput) {
	rec.Filename = in.Filename
	rec.Format = in.Format
	rec.ByteSize = in.ByteSize
	rec.CapturedAt = in.CapturedAt
	rec.IsRaw = in.IsRaw
}

func sameImage(rec protocol.ImageRecord, in protocol.ImageInput) bool {
	return rec.Filename == in.Filename &&
		rec.Format == in.Format &&
		rec.ByteSize == in.ByteSize &&
		rec.CapturedAt.Equal(in.CapturedAt) &&
		rec.IsRaw == in.IsRaw
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
