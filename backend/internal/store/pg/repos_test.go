package pg_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hauph/camera/backend/internal/auth"
	"github.com/hauph/camera/backend/internal/billing"
	"github.com/hauph/camera/backend/internal/secrets"
	"github.com/hauph/camera/backend/internal/storage"
	"github.com/hauph/camera/backend/internal/storage/gdrive"
	"github.com/hauph/camera/backend/internal/store/pg"
)

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	s, ok := newStore(t).(*pgStore)
	if !ok {
		t.Fatal("không lấy được pool")
	}
	return s.pool
}

func newCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("sinh khoá: %v", err)
	}
	c, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

// --- auth ---

// TestHiddenEmailUsersDoNotCollide là ràng buộc mà migration 0002 tồn tại để
// đáp ứng: người dùng Sign in with Apple được phép ẩn email HOÀN TOÀN.
//
// Bản in-memory không phát hiện được vấn đề này vì nó không có ràng buộc UNIQUE
// thật. Đây là loại khác biệt mà chỉ test với Postgres thật mới bắt được.
func TestHiddenEmailUsersDoNotCollide(t *testing.T) {
	ctx := context.Background()
	repo := pg.NewAuthRepo(newPool(t), time.Now)

	var created []auth.User
	for i := 0; i < 3; i++ {
		u, err := repo.CreateUser(ctx, "", "Người ẩn danh", false)
		if err != nil {
			t.Fatalf("tạo người dùng ẩn email thứ %d: %v", i+1, err)
		}
		created = append(created, u)
	}

	// Ba người khác nhau, không được gộp thành một.
	if created[0].ID == created[1].ID || created[1].ID == created[2].ID {
		t.Error("người dùng ẩn email bị gộp vào cùng một tài khoản")
	}

	// Và tra theo email rỗng KHÔNG được trả về ai cả — nếu trả, mọi người ẩn
	// email sẽ bị ghép nhầm vào người đầu tiên.
	if _, err := repo.UserByEmail(ctx, ""); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("tra email rỗng trả %v, muốn ErrUserNotFound", err)
	}
}

func TestDuplicateEmailRejectedAtDatabase(t *testing.T) {
	ctx := context.Background()
	repo := pg.NewAuthRepo(newPool(t), time.Now)

	if _, err := repo.CreateUser(ctx, "a@example.com", "A", true); err != nil {
		t.Fatalf("tạo lần 1: %v", err)
	}
	// Ràng buộc phải nằm ở CƠ SỞ DỮ LIỆU, không chỉ ở tầng ứng dụng: hai request
	// đồng thời đều thấy email chưa tồn tại rồi cùng chèn.
	if _, err := repo.CreateUser(ctx, "a@example.com", "B", true); !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("tạo lần 2 trả %v, muốn ErrEmailTaken", err)
	}
}

// TestAuthServiceEndToEnd chạy toàn bộ auth.Service trên Postgres thật.
func TestAuthServiceEndToEnd(t *testing.T) {
	ctx := context.Background()
	repo := pg.NewAuthRepo(newPool(t), time.Now)
	svc := auth.NewService(repo, map[auth.Provider]*auth.Verifier{}, time.Now)

	token, user, err := svc.SignUpWithPassword(ctx, "Nguoi.Dung@Example.COM", "mat-khau-du-dai-12", "Người Dùng")
	if err != nil {
		t.Fatalf("đăng ký: %v", err)
	}
	if user.Email != "nguoi.dung@example.com" {
		t.Errorf("email = %q, muốn dạng chữ thường", user.Email)
	}

	got, err := svc.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("xác thực: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("id = %q, muốn %q", got.ID, user.ID)
	}

	// Phiên thứ hai từ thiết bị khác.
	token2, _, err := svc.SignInWithPassword(ctx, "nguoi.dung@example.com", "mat-khau-du-dai-12")
	if err != nil {
		t.Fatalf("đăng nhập: %v", err)
	}

	// Đăng xuất mọi nơi phải vô hiệu hoá cả hai — khả năng mà JWT tự ký không có.
	if err := svc.SignOutEverywhere(ctx, user.ID); err != nil {
		t.Fatalf("đăng xuất mọi nơi: %v", err)
	}
	for i, tok := range []string{token, token2} {
		if _, err := svc.Authenticate(ctx, tok); !errors.Is(err, auth.ErrSessionInvalid) {
			t.Errorf("token %d vẫn dùng được sau khi đăng xuất mọi nơi", i+1)
		}
	}
}

func TestPurgeExpiredSessions(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	repo := pg.NewAuthRepo(pool, time.Now)

	u, err := repo.CreateUser(ctx, "a@example.com", "A", true)
	if err != nil {
		t.Fatalf("tạo user: %v", err)
	}
	live := []byte("hash-con-han-0000000000000000000")
	dead := []byte("hash-het-han-0000000000000000000")
	if err := repo.CreateSession(ctx, u.ID, live, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("phiên còn hạn: %v", err)
	}
	if err := repo.CreateSession(ctx, u.ID, dead, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("phiên hết hạn: %v", err)
	}

	n, err := repo.PurgeExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("PurgeExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("dọn %d phiên, muốn 1", n)
	}
	if _, err := repo.SessionByTokenHash(ctx, live); err != nil {
		t.Errorf("phiên còn hạn bị dọn nhầm: %v", err)
	}
}

// --- storage ---

// TestRefreshTokenIsEncryptedAtRest là test quan trọng nhất của StorageRepo.
//
// Nó đọc THẲNG cột trong cơ sở dữ liệu và khẳng định không tìm thấy chuỗi gốc.
// Không có test này, một lỗi kiểu "quên gọi Encrypt" sẽ không bị phát hiện — mọi
// thứ vẫn chạy đúng, chỉ là token nằm dạng thô. Và refresh token lộ nghĩa là kẻ
// tấn công truy cập Drive của mọi người dùng đã liên kết, không thu hồi được
// bằng cách đổi mật khẩu.
func TestRefreshTokenIsEncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	authRepo := pg.NewAuthRepo(pool, time.Now)
	repo := pg.NewStorageRepo(pool, newCipher(t), time.Now)

	u, err := authRepo.CreateUser(ctx, "a@example.com", "A", true)
	if err != nil {
		t.Fatalf("tạo user: %v", err)
	}

	const plaintext = "1//0gRefreshTokenRatDaiVaBiMat"
	if err := repo.SaveRefreshToken(ctx, u.ID, plaintext); err != nil {
		t.Fatalf("SaveRefreshToken: %v", err)
	}

	var raw []byte
	err = pool.QueryRow(ctx, `
		SELECT refresh_token_enc FROM storage_links
		WHERE user_id = $1 AND provider = 'google_drive'`, u.ID).Scan(&raw)
	if err != nil {
		t.Fatalf("đọc cột: %v", err)
	}
	if bytes.Contains(raw, []byte(plaintext)) {
		t.Fatal("REFRESH TOKEN NẰM DẠNG THÔ trong cơ sở dữ liệu")
	}
	if len(raw) == 0 {
		t.Fatal("cột rỗng")
	}

	// Và đọc lại phải ra đúng chuỗi gốc.
	got, err := repo.RefreshToken(ctx, u.ID)
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if got != plaintext {
		t.Errorf("giải mã ra %q, muốn %q", got, plaintext)
	}
}

// TestRefreshTokenCannotBeMovedBetweenUsers: kẻ tấn công có quyền ghi vào cơ sở
// dữ liệu chép bản mã của nạn nhân sang dòng của mình. Không có ràng buộc ngữ
// cảnh, server sẽ giải mã bình thường và truy cập Drive nạn nhân thay mặt hắn.
func TestRefreshTokenCannotBeMovedBetweenUsers(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	authRepo := pg.NewAuthRepo(pool, time.Now)
	repo := pg.NewStorageRepo(pool, newCipher(t), time.Now)

	victim, _ := authRepo.CreateUser(ctx, "nan-nhan@example.com", "V", true)
	attacker, _ := authRepo.CreateUser(ctx, "ke-tan-cong@example.com", "A", true)

	if err := repo.SaveRefreshToken(ctx, victim.ID, "token-cua-nan-nhan"); err != nil {
		t.Fatalf("lưu token: %v", err)
	}

	// Chép bản mã sang dòng của kẻ tấn công, đúng như khi có quyền ghi vào DB.
	var enc []byte
	if err := pool.QueryRow(ctx,
		`SELECT refresh_token_enc FROM storage_links WHERE user_id = $1`, victim.ID).Scan(&enc); err != nil {
		t.Fatalf("đọc bản mã: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_links (user_id, provider, refresh_token_enc)
		VALUES ($1, 'google_drive', $2)`, attacker.ID, enc); err != nil {
		t.Fatalf("chèn bản mã đã chép: %v", err)
	}

	if _, err := repo.RefreshToken(ctx, attacker.ID); !errors.Is(err, gdrive.ErrNoRefreshToken) {
		t.Fatalf("GIẢI MÃ ĐƯỢC token của người khác — lỗi = %v", err)
	}
}

func TestRefusesToStoreTokenWithoutCipher(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	authRepo := pg.NewAuthRepo(pool, time.Now)
	// cipher = nil: chưa cấu hình khoá.
	repo := pg.NewStorageRepo(pool, nil, time.Now)

	u, _ := authRepo.CreateUser(ctx, "a@example.com", "A", true)
	if err := repo.SaveRefreshToken(ctx, u.ID, "token"); err == nil {
		t.Fatal("lưu refresh token dạng thô khi chưa cấu hình khoá mã hoá")
	}
}

// TestUsageIsAtomic: hai upload hoàn tất cùng lúc phải cộng đủ cả hai. Đọc-rồi-
// ghi từ Go sẽ làm mất một lần cộng, và mất mát đó tích luỹ một chiều cho tới khi
// người dùng dùng được nhiều hơn mức đã mua.
func TestUsageIsAtomic(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	authRepo := pg.NewAuthRepo(pool, time.Now)
	repo := pg.NewStorageRepo(pool, newCipher(t), time.Now)

	u, _ := authRepo.CreateUser(ctx, "a@example.com", "A", true)

	const n = 50
	const each = 1024
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { errs <- repo.Add(ctx, u.ID, each) }()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	used, err := repo.Used(ctx, u.ID)
	if err != nil {
		t.Fatalf("Used: %v", err)
	}
	if used != n*each {
		t.Errorf("đã dùng = %d, muốn %d — mất %d byte do cộng không nguyên tử",
			used, n*each, n*each-used)
	}

	// Trừ quá tay không được ra số âm.
	if err := repo.Add(ctx, u.ID, -1_000_000); err != nil {
		t.Fatalf("Add âm: %v", err)
	}
	if used, _ := repo.Used(ctx, u.ID); used != 0 {
		t.Errorf("đã dùng = %d sau khi trừ quá tay, muốn 0", used)
	}
}

func TestSelectionDefaultsToDevice(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	authRepo := pg.NewAuthRepo(pool, time.Now)
	repo := pg.NewStorageRepo(pool, newCipher(t), time.Now)

	u, _ := authRepo.CreateUser(ctx, "a@example.com", "A", true)

	// Chưa chọn gì: mặc định device — ảnh ở lại trên thẻ, không tốn gì của ai.
	got, err := repo.Selected(u.ID)
	if err != nil {
		t.Fatalf("Selected: %v", err)
	}
	if got != storage.ProviderDevice {
		t.Errorf("mặc định = %q, muốn device", got)
	}

	if err := repo.Select(u.ID, storage.ProviderManaged); err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got, _ := repo.Selected(u.ID); got != storage.ProviderManaged {
		t.Errorf("sau khi chọn = %q, muốn managed", got)
	}
}

// --- billing ---

// TestTransactionOwnerIsImmutable: ràng buộc phải nằm ở CƠ SỞ DỮ LIỆU, không chỉ
// ở tầng ứng dụng. Nếu ON CONFLICT cập nhật cả user_id, kẻ tấn công gửi lại hoá
// đơn của người khác sẽ cướp được quyền lợi ngay ở tầng SQL, bỏ qua mọi kiểm tra
// phía Go.
func TestTransactionOwnerIsImmutable(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	authRepo := pg.NewAuthRepo(pool, time.Now)
	repo := pg.NewBillingRepo(pool, time.Now)

	buyer, _ := authRepo.CreateUser(ctx, "nguoi-mua@example.com", "B", true)
	other, _ := authRepo.CreateUser(ctx, "nguoi-khac@example.com", "O", true)

	exp := time.Now().Add(30 * 24 * time.Hour)
	base := billing.Entitlement{
		Platform: billing.PlatformApple, TransactionID: "tx-1", UserID: buyer.ID,
		ProductID: "storage_1tb_monthly", StorageBytes: 1024 * (1 << 30), ExpiresAt: exp,
	}
	if err := repo.Upsert(ctx, base); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Ghi đè với user_id khác — đúng như một lỗi lập trình hoặc một cuộc tấn công
	// vượt qua tầng Go.
	hijack := base
	hijack.UserID = other.ID
	if err := repo.Upsert(ctx, hijack); err != nil {
		t.Fatalf("Upsert lần 2: %v", err)
	}

	got, err := repo.ByTransaction(ctx, billing.PlatformApple, "tx-1")
	if err != nil {
		t.Fatalf("ByTransaction: %v", err)
	}
	if got.UserID != buyer.ID {
		t.Errorf("chủ sở hữu đổi thành %q — một giao dịch đã thuộc về ai thì phải thuộc về người đó vĩnh viễn", got.UserID)
	}

	ofOther, _ := repo.OfUser(ctx, other.ID)
	if len(ofOther) != 0 {
		t.Errorf("tài khoản khác nhận được %d quyền lợi", len(ofOther))
	}
}

func TestBillingServiceEndToEnd(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	authRepo := pg.NewAuthRepo(pool, time.Now)
	repo := pg.NewBillingRepo(pool, time.Now)

	u, _ := authRepo.CreateUser(ctx, "a@example.com", "A", true)
	exp := time.Now().Add(30 * 24 * time.Hour)

	svc := billing.NewService(repo, fakeVerifier{Purchase: billing.Purchase{
		Platform: billing.PlatformApple, TransactionID: "tx-1",
		ProductID: "storage_100gb_monthly", ExpiresAt: exp,
	}}, billing.DefaultCatalog(), time.Now)

	if _, err := svc.Redeem(ctx, u.ID, billing.PlatformApple, "hoa-don"); err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	q, err := svc.QuotaBytes(ctx, u.ID)
	if err != nil {
		t.Fatalf("QuotaBytes: %v", err)
	}
	want := int64(billing.FreeQuotaBytes) + 100*billing.GiB
	if q != want {
		t.Errorf("hạn mức = %d GiB, muốn %d GiB", q/billing.GiB, want/billing.GiB)
	}

	// Phát lại không cộng dồn.
	if _, err := svc.Redeem(ctx, u.ID, billing.PlatformApple, "hoa-don"); err != nil {
		t.Fatalf("Redeem lần 2: %v", err)
	}
	if again, _ := svc.QuotaBytes(ctx, u.ID); again != want {
		t.Errorf("sau khi phát lại hạn mức = %d GiB, muốn %d GiB", again/billing.GiB, want/billing.GiB)
	}
}

type fakeVerifier struct{ billing.Purchase }

func (f fakeVerifier) Verify(context.Context, billing.Platform, string) (billing.Purchase, error) {
	return f.Purchase, nil
}
