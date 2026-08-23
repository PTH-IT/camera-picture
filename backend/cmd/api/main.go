// Command api là backend của app.
//
// Phạm vi (xem docs/adr/0001-capture-strategy.md): backend KHÔNG làm capture.
// Capture chạy trên điện thoại qua CascableCore. Backend giữ tài khoản, đồng bộ,
// render RAW khi xuất, điều phối AI, phân phối preset, và lưu trữ dài hạn.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hauph/camera/backend/internal/api"
	"github.com/hauph/camera/backend/internal/store"
	"github.com/hauph/camera/backend/internal/store/memory"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// Hiện chỉ có bản lưu trong bộ nhớ. Bản Postgres theo lược đồ ở
	// migrations/0001_init.sql là việc tiếp theo của tầng store.
	//
	// Chạy với store này thì DỮ LIỆU MẤT KHI TẮT. Cảnh báo rõ lúc khởi động thay
	// vì để ai đó tưởng nhầm là bản chạy thật.
	log.Warn("đang dùng store trong bộ nhớ — dữ liệu sẽ mất khi tắt tiến trình")
	st := memory.New(newID, time.Now)

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.New(st, log).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		// Không đặt WriteTimeout toàn cục: upload RAW là 50-60MB mỗi file và có
		// thể rất chậm qua mạng di động. Timeout đặt theo từng handler.
	}

	go func() {
		log.Info("api đang lắng nghe", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("lắng nghe thất bại", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("tắt không sạch", "err", err)
	}
	log.Info("đã dừng")
}

// newID sinh định danh.
//
// Tạm dùng bộ đếm kèm dấu thời gian để tránh thêm phụ thuộc khi chưa cần. Khi
// chuyển sang Postgres, đổi sang UUID v7 — lược đồ đã khai báo cột uuid, và v7
// sắp xếp theo thời gian nên chỉ mục không bị phân mảnh như v4.
var idCounter int64

func newID() string {
	idCounter++
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), idCounter)
}

var _ store.Store = (*memory.Store)(nil)
