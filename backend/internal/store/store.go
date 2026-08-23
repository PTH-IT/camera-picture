// Package store định nghĩa tầng lưu trữ của backend.
//
// Có interface thay vì gọi thẳng Postgres không phải để "dễ đổi database" — đó
// là lý do thường được viện dẫn và hiếm khi thành sự thật. Lý do thật: logic đồng
// bộ delta là phần dễ sai nhất và đắt nhất khi sai (client mất ảnh, hoặc lặp vô
// hạn), nên nó cần được test kỹ mà không cần dựng Postgres. Bản in-memory trong
// store/memory tồn tại cho mục đích đó.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/hauph/camera/backend/internal/protocol"
)

var (
	ErrNotFound = errors.New("không tìm thấy")
	ErrConflict = errors.New("xung đột")
)

type Session struct {
	ID         string
	UserID     string
	Name       string
	ClientName string
	StartedAt  time.Time
	// Revision là đồng hồ logic của buổi chụp. Xem chú thích trong
	// migrations/0001_init.sql về việc vì sao không dùng timestamp.
	Revision int64
}

// BatchResult tách created và updated để client biết lô vừa gửi có thật sự thay
// đổi gì không — hữu ích khi chẩn đoán vòng lặp retry.
type BatchResult struct {
	IDs      map[string]string
	Created  int
	Updated  int
	Revision int64
}

type Store interface {
	CreateSession(ctx context.Context, userID, name, clientName string, startedAt time.Time) (Session, error)
	GetSession(ctx context.Context, sessionID string) (Session, error)

	// BatchUpsertImages ghi metadata một lô ảnh. PHẢI idempotent theo ClientID:
	// buổi chụp thật hay rớt mạng và client buộc phải retry mù.
	BatchUpsertImages(ctx context.Context, sessionID string, in []protocol.ImageInput) (BatchResult, error)

	// Changes trả về mọi thay đổi có revision > since, sắp xếp tăng dần theo
	// revision, tối đa limit bản ghi.
	//
	// Hợp đồng quan trọng: mỗi bản ghi thay đổi mang một revision RIÊNG BIỆT.
	// Nếu nhiều bản ghi dùng chung một revision, việc phân trang sẽ bỏ sót —
	// client lấy được nửa nhóm, đặt con trỏ bằng revision đó, rồi nửa còn lại
	// không bao giờ thoả điều kiện "> since". Đó là kiểu lỗi mất ảnh âm thầm.
	Changes(ctx context.Context, sessionID string, since int64, limit int) (protocol.ChangesResponse, error)

	PutEdit(ctx context.Context, imageID string, in protocol.PutEditRequest) (protocol.EditRecord, error)
	ConfirmAsset(ctx context.Context, imageID string, in protocol.ConfirmAssetRequest) error

	GetImage(ctx context.Context, imageID string) (protocol.ImageRecord, error)
	// SessionOfImage cần cho việc phân quyền và cấp phát revision.
	SessionOfImage(ctx context.Context, imageID string) (string, error)

	SoftDeleteImage(ctx context.Context, imageID string) error
}

// IDGen tách ra để test sinh id xác định, thay vì phải so khớp UUID ngẫu nhiên.
type IDGen func() string

// Clock tách ra vì lý do tương tự. Đừng gọi time.Now() trực tiếp trong tầng store.
type Clock func() time.Time
