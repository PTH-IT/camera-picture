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
	"strings"
	"syscall"
	"time"

	"github.com/hauph/camera/backend/internal/api"
	"github.com/hauph/camera/backend/internal/auth"
	"github.com/hauph/camera/backend/internal/auth/memrepo"
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

	// Client id của Apple/Google lấy từ biến môi trường. Không có thì chỉ còn
	// đăng nhập bằng mật khẩu — cảnh báo rõ thay vì để đăng nhập xã hội im lặng
	// trả 500 lúc chạy.
	verifiers := map[auth.Provider]*auth.Verifier{}
	if ids := splitEnv("GOOGLE_CLIENT_IDS"); len(ids) > 0 {
		verifiers[auth.ProviderGoogle] = auth.NewGoogleVerifier(ids, nil)
	} else {
		log.Warn("thiếu GOOGLE_CLIENT_IDS — đăng nhập Google bị tắt")
	}
	if ids := splitEnv("APPLE_CLIENT_IDS"); len(ids) > 0 {
		verifiers[auth.ProviderApple] = auth.NewAppleVerifier(ids, nil)
	} else {
		// Nhắc lại ràng buộc của App Store: có đăng nhập Google thì Sign in with
		// Apple là bắt buộc, không phải tuỳ chọn. Xem ADR 0002.
		log.Warn("thiếu APPLE_CLIENT_IDS — đăng nhập Apple bị tắt; " +
			"App Store guideline 4.8 bắt buộc có Apple nếu đã có Google")
	}
	authSvc := auth.NewService(memrepo.New(time.Now), verifiers, time.Now)

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.New(st, authSvc, api.StorageDeps{}, log).Routes(),
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

// splitEnv đọc danh sách phân tách bằng dấu phẩy. Nhiều client id vì iOS, Android
// và web thường được cấp riêng.
func splitEnv(name string) []string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var _ store.Store = (*memory.Store)(nil)
