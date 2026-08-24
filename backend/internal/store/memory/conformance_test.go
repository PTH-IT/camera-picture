package memory_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hauph/camera/backend/internal/store"
	"github.com/hauph/camera/backend/internal/store/memory"
	"github.com/hauph/camera/backend/internal/store/storetest"
)

// Bản in-memory chạy qua ĐÚNG bộ test tuân thủ mà bản Postgres chạy. Xem
// chú thích của package storetest về lý do.
func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		n := 0
		base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
		tick := 0
		return memory.New(
			func() string { n++; return fmt.Sprintf("id-%06d", n) },
			func() time.Time { tick++; return base.Add(time.Duration(tick) * time.Second) },
		)
	})
}
