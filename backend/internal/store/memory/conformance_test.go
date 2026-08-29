package memory_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hauph/camera/backend/internal/store"
	"github.com/hauph/camera/backend/internal/store/memory"
	"github.com/hauph/camera/backend/internal/store/storetest"
)

// memStore cấp id người dùng cho bộ test tuân thủ.
//
// Bản pg tạo một user MỚI cho mỗi lần gọi (nó phải làm vậy vì có khoá ngoại),
// nên bản in-memory cũng phải thế. Trả về cùng một id cho mọi lần gọi sẽ khiến
// các test về cô lập dữ liệu giữa hai người dùng luôn xanh ở đây và chỉ đỏ khi
// chạy với Postgres — đúng kiểu lệch hành vi mà bộ test tuân thủ sinh ra để
// chặn.
type memStore struct {
	*memory.Store
	users int
}

func (m *memStore) TestUserID(t *testing.T) string {
	t.Helper()
	m.users++
	return fmt.Sprintf("user-%d", m.users)
}

// Bản in-memory chạy qua ĐÚNG bộ test tuân thủ mà bản Postgres chạy. Xem
// chú thích của package storetest về lý do.
func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		n := 0
		base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
		tick := 0
		return &memStore{Store: memory.New(
			func() string { n++; return fmt.Sprintf("id-%06d", n) },
			func() time.Time { tick++; return base.Add(time.Duration(tick) * time.Second) },
		)}
	})
}
