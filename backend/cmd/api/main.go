// Command api là backend của app.
//
// Phạm vi (xem docs/adr/0001-capture-strategy.md): backend KHÔNG làm capture.
// Capture chạy trên điện thoại qua CascableCore. Backend giữ tài khoản, đồng bộ,
// render RAW khi xuất, điều phối AI, phân phối preset, và lưu trữ dài hạn.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hauph/camera/backend/internal/api"
	"github.com/hauph/camera/backend/internal/auth"
	"github.com/hauph/camera/backend/internal/auth/memrepo"
	"github.com/hauph/camera/backend/internal/billing"
	"github.com/hauph/camera/backend/internal/billing/appstore"
	"github.com/hauph/camera/backend/internal/envfile"
	"github.com/hauph/camera/backend/internal/ids"
	"github.com/hauph/camera/backend/internal/migrate"
	"github.com/hauph/camera/backend/internal/secrets"
	"github.com/hauph/camera/backend/internal/storage"
	"github.com/hauph/camera/backend/internal/storage/gdrive"
	"github.com/hauph/camera/backend/internal/storage/miniostore"
	"github.com/hauph/camera/backend/internal/store"
	"github.com/hauph/camera/backend/internal/store/memory"
	"github.com/hauph/camera/backend/internal/store/pg"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Nạp .env nếu có. Biến môi trường đã đặt sẵn LUÔN thắng file, nên trên
	// production và CI cấu hình thật không bao giờ bị một file lỡ tay commit đè lên.
	if err := envfile.Load(); err != nil {
		log.Warn("không đọc được .env", "err", err)
	}

	if err := run(log); err != nil {
		log.Error("khởi động thất bại", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handler, cleanup, err := wire(ctx, log)
	if err != nil {
		return err
	}
	defer cleanup()

	addr := env("API_ADDR", ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Không đặt WriteTimeout toàn cục: upload RAW là 50-60MB mỗi file và có
		// thể rất chậm qua mạng di động. Timeout đặt theo từng handler.
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("api đang lắng nghe", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("tắt không sạch", "err", err)
	}
	log.Info("đã dừng")
	return nil
}

// wire dựng toàn bộ phụ thuộc từ biến môi trường.
//
// Nguyên tắc xuyên suốt: THIẾU CẤU HÌNH THÌ TẮT TÍNH NĂNG VÀ CẢNH BÁO RÕ, không
// sập lúc khởi động và cũng không im lặng. Endpoint của tính năng bị tắt trả 501
// kèm mã not_configured, nên client ẩn nút thay vì hiện lỗi.
//
// Ngoại lệ là những cấu hình mà chạy tiếp NGUY HIỂM hơn dừng lại — ví dụ bật
// Drive mà không có khoá mã hoá, vì khi đó refresh token sẽ nằm dạng thô.
func wire(ctx context.Context, log *slog.Logger) (http.Handler, func(), error) {
	var cleanups []func()
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	var (
		mainStore store.Store
		authRepo  auth.Repo
		billRepo  billing.Repo
		storeRepo *pg.StorageRepo
	)

	// Đọc khoá mã hoá trước, vì quyết định có bật Drive hay không phụ thuộc nó.
	var cipher *secrets.Cipher
	if os.Getenv("STORAGE_SECRET_KEY") != "" {
		c, err := secrets.FromEnv("STORAGE_SECRET_KEY")
		if err != nil {
			return nil, cleanup, err
		}
		cipher = c
	}

	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			return nil, cleanup, err
		}
		cleanups = append(cleanups, pool.Close)
		if err := pool.Ping(ctx); err != nil {
			return nil, cleanup, err
		}

		// Chạy migration lúc khởi động. Advisory lock trong migrate.Run xử lý
		// trường hợp nhiều bản sao cùng khởi động khi rolling deploy.
		if err := migrate.Run(ctx, pool); err != nil {
			return nil, cleanup, err
		}
		applied, _ := migrate.Applied(ctx, pool)
		log.Info("cơ sở dữ liệu sẵn sàng", "migrations", len(applied))

		mainStore = pg.NewStore(pool, time.Now)
		authRepo = pg.NewAuthRepo(pool, time.Now)
		billRepo = pg.NewBillingRepo(pool, time.Now)
		storeRepo = pg.NewStorageRepo(pool, cipher, time.Now)
	} else {
		// Cảnh báo phải rất rõ: chạy như thế này thì mọi dữ liệu mất khi tắt
		// tiến trình, và không ai muốn phát hiện điều đó sau một buổi chụp thật.
		log.Warn("KHÔNG có DATABASE_URL — dùng store trong bộ nhớ, DỮ LIỆU SẼ MẤT KHI TẮT")
		mainStore = memory.New(ids.New, time.Now)
		authRepo = memrepo.New(time.Now)
		billRepo = billing.NewMemRepo()
	}

	// --- xác thực ---
	verifiers := map[auth.Provider]*auth.Verifier{}
	if v := splitEnv("GOOGLE_CLIENT_IDS"); len(v) > 0 {
		verifiers[auth.ProviderGoogle] = auth.NewGoogleVerifier(v, nil)
	} else {
		log.Warn("thiếu GOOGLE_CLIENT_IDS — đăng nhập Google bị tắt")
	}
	if v := splitEnv("APPLE_CLIENT_IDS"); len(v) > 0 {
		verifiers[auth.ProviderApple] = auth.NewAppleVerifier(v, nil)
	} else {
		// Nhắc lại ràng buộc App Store: có đăng nhập Google thì Sign in with
		// Apple là bắt buộc, không phải tuỳ chọn. Xem ADR 0002.
		log.Warn("thiếu APPLE_CLIENT_IDS — đăng nhập Apple bị tắt; " +
			"App Store guideline 4.8 bắt buộc có Apple nếu đã có Google")
	}
	authSvc := auth.NewService(authRepo, verifiers, time.Now)

	// --- thanh toán ---
	//
	// Mặc định là TỪ CHỐI mọi hoá đơn chứ không phải chấp nhận mọi hoá đơn. Một
	// bản chấp nhận tất cả sẽ cho bất kỳ ai tự cấp dung lượng không giới hạn, và
	// lỗi đó không có triệu chứng nào ngoài hoá đơn hạ tầng tăng vọt.
	var (
		receiptVerifier billing.ReceiptVerifier = rejectAllReceipts{}
		appleVerifier   *appstore.Verifier
	)
	if certFile := os.Getenv("APPLE_ROOT_CERT_FILE"); certFile != "" {
		bundleID := os.Getenv("APPLE_BUNDLE_ID")
		if bundleID == "" {
			return nil, cleanup, errors.New(
				"APPLE_ROOT_CERT_FILE được đặt nhưng thiếu APPLE_BUNDLE_ID — " +
					"không kiểm bundle id thì hoá đơn hợp lệ của app KHÁC cũng được chấp nhận")
		}
		pemBytes, err := os.ReadFile(certFile)
		if err != nil {
			return nil, cleanup, err
		}
		env := os.Getenv("APPLE_ENVIRONMENT")
		if env == "" {
			// Không mặc định về rỗng (chấp nhận cả hai): giao dịch Sandbox miễn
			// phí và ai có tài khoản nhà phát triển cũng tạo được.
			env = "Production"
		}
		v, err := appstore.New(appstore.Config{
			AppleRootCertsPEM: pemBytes,
			BundleID:          bundleID,
			Environment:       env,
			Now:               time.Now,
		})
		if err != nil {
			return nil, cleanup, err
		}
		appleVerifier = v
		receiptVerifier = appstore.NewReceiptVerifier(v)
		log.Info("xác minh hoá đơn App Store sẵn sàng", "bundleId", bundleID, "environment", env)
	} else {
		log.Warn("thiếu APPLE_ROOT_CERT_FILE — mọi hoá đơn mua dung lượng đều bị từ chối")
	}

	billSvc := billing.NewService(billRepo, receiptVerifier, billing.DefaultCatalog(), time.Now)

	// --- lưu trữ ---
	sd := api.StorageDeps{Billing: billSvc}
	if appleVerifier != nil {
		sd.AppleNotifications = appleVerifier
	}
	var providers []storage.Provider

	if ep := os.Getenv("MINIO_ENDPOINT"); ep != "" {
		if storeRepo == nil {
			return nil, cleanup, errors.New(
				"MINIO_ENDPOINT được đặt nhưng thiếu DATABASE_URL — " +
					"không theo dõi được dung lượng đã dùng thì hạn mức là vô nghĩa")
		}
		m, err := miniostore.New(miniostore.Config{
			Endpoint:  ep,
			AccessKey: os.Getenv("MINIO_ACCESS_KEY"),
			SecretKey: os.Getenv("MINIO_SECRET_KEY"),
			Bucket:    env("MINIO_BUCKET", "camera"),
			UseSSL:    os.Getenv("MINIO_USE_SSL") == "true",
		}, billSvc.QuotaBytes, storeRepo)
		if err != nil {
			return nil, cleanup, err
		}
		if err := m.EnsureBucket(ctx); err != nil {
			return nil, cleanup, err
		}
		providers = append(providers, m)
		log.Info("provider managed sẵn sàng", "endpoint", ep)
	} else {
		log.Warn("thiếu MINIO_ENDPOINT — lưu trữ do máy chủ quản lý bị tắt")
	}

	if id := os.Getenv("GOOGLE_DRIVE_CLIENT_ID"); id != "" {
		switch {
		case cipher == nil:
			// Dừng hẳn chứ không tắt tính năng: chạy tiếp nghĩa là refresh token
			// nằm dạng thô trong cơ sở dữ liệu, và token đó cho quyền đọc ghi
			// Drive của người dùng gần như vô thời hạn.
			return nil, cleanup, errors.New(
				"GOOGLE_DRIVE_CLIENT_ID được đặt nhưng thiếu STORAGE_SECRET_KEY — " +
					"từ chối chạy vì refresh token sẽ không được mã hoá")
		case storeRepo == nil:
			return nil, cleanup, errors.New(
				"GOOGLE_DRIVE_CLIENT_ID được đặt nhưng thiếu DATABASE_URL — " +
					"không có chỗ lưu refresh token bền vững")
		}
		d := gdrive.New(gdrive.Config{
			ClientID:     id,
			ClientSecret: os.Getenv("GOOGLE_DRIVE_CLIENT_SECRET"),
			RedirectURI:  os.Getenv("GOOGLE_DRIVE_REDIRECT_URI"),
			FolderName:   env("GOOGLE_DRIVE_FOLDER", "Camera Picture"),
		}, storeRepo, nil)
		providers = append(providers, d)
		sd.Drive = d
		log.Info("provider Google Drive sẵn sàng", "scope", gdrive.ScopeDriveFile)
	} else {
		log.Warn("thiếu GOOGLE_DRIVE_CLIENT_ID — liên kết Drive bị tắt")
	}

	if len(providers) > 0 {
		sd.Registry = storage.NewRegistry(providers...)
	}
	if storeRepo != nil {
		sd.Selection = storeRepo
	}

	return api.New(mainStore, authSvc, sd, log).Routes(), cleanup, nil
}

// rejectAllReceipts là ReceiptVerifier tạm thời cho tới khi nối App Store Server
// API và Google Play Developer API. Xem chú thích tại chỗ dùng.
type rejectAllReceipts struct{}

func (rejectAllReceipts) Verify(context.Context, billing.Platform, string) (billing.Purchase, error) {
	return billing.Purchase{}, errors.New("chưa cấu hình xác minh hoá đơn với store")
}

func env(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
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
