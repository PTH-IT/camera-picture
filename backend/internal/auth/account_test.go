package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hauph/camera/backend/internal/auth"
	"github.com/hauph/camera/backend/internal/auth/memrepo"
)

// Bộ test này tập trung vào các QUYẾT ĐỊNH BẢO MẬT của tầng tài khoản, không phải
// vào việc CRUD chạy đúng. Quy tắc ghép tài khoản và vòng đời phiên là hai chỗ mà
// một lỗi đồng nghĩa với việc chiếm được tài khoản người khác.

func newSvc(t *testing.T) (*auth.Service, *memrepo.Repo) {
	t.Helper()
	repo := memrepo.New(time.Now)
	// Không có verifier: các test ở đây gọi thẳng resolveUser qua đường mật khẩu
	// và qua LinkIdentity. Đường OIDC đã được test riêng trong oidc_test.go.
	return auth.NewService(repo, map[auth.Provider]*auth.Verifier{}, time.Now), repo
}

func TestSignUpAndSignIn(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSvc(t)

	token, user, err := svc.SignUpWithPassword(ctx, "  Nguoi.Dung@Example.COM ", "mat-khau-du-dai-12", "Người Dùng")
	if err != nil {
		t.Fatalf("đăng ký: %v", err)
	}
	// Email phải được chuẩn hoá, nếu không cùng một người gõ hoa/thường khác nhau
	// sẽ tạo ra hai tài khoản.
	if user.Email != "nguoi.dung@example.com" {
		t.Errorf("Email = %q, muốn dạng chữ thường đã cắt khoảng trắng", user.Email)
	}

	got, err := svc.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("xác thực bằng token vừa cấp: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("ID = %q, muốn %q", got.ID, user.ID)
	}

	// Đăng nhập lại với cách viết hoa khác phải ra cùng tài khoản.
	_, again, err := svc.SignInWithPassword(ctx, "NGUOI.DUNG@EXAMPLE.COM", "mat-khau-du-dai-12")
	if err != nil {
		t.Fatalf("đăng nhập lại: %v", err)
	}
	if again.ID != user.ID {
		t.Errorf("đăng nhập lại ra tài khoản khác: %q vs %q", again.ID, user.ID)
	}
}

func TestWrongPasswordRejected(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSvc(t)

	if _, _, err := svc.SignUpWithPassword(ctx, "a@example.com", "mat-khau-du-dai-12", "A"); err != nil {
		t.Fatalf("đăng ký: %v", err)
	}
	_, _, err := svc.SignInWithPassword(ctx, "a@example.com", "mat-khau-sai-nhung-dai")
	if !errors.Is(err, auth.ErrWrongCredentials) {
		t.Errorf("lỗi = %v, muốn ErrWrongCredentials", err)
	}
}

// TestErrorDoesNotRevealAccountExistence: thông báo lỗi phải giống hệt nhau giữa
// "email chưa đăng ký" và "sai mật khẩu". Khác nhau là biến form đăng nhập thành
// công cụ dò xem ai đã dùng dịch vụ.
func TestErrorDoesNotRevealAccountExistence(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSvc(t)

	if _, _, err := svc.SignUpWithPassword(ctx, "co-that@example.com", "mat-khau-du-dai-12", ""); err != nil {
		t.Fatalf("đăng ký: %v", err)
	}

	_, _, errExisting := svc.SignInWithPassword(ctx, "co-that@example.com", "sai-mat-khau-dai")
	_, _, errMissing := svc.SignInWithPassword(ctx, "khong-ton-tai@example.com", "sai-mat-khau-dai")

	if errExisting.Error() != errMissing.Error() {
		t.Errorf("thông báo lỗi khác nhau:\n  tài khoản có thật: %v\n  không tồn tại:     %v",
			errExisting, errMissing)
	}
}

// TestOIDCOnlyAccountDoesNotLeakViaPasswordLogin: tài khoản chỉ đăng nhập bằng
// Google mà bị đăng nhập mật khẩu vẫn phải trả lỗi CHUNG. Nói "tài khoản này dùng
// Google" là tiết lộ cả sự tồn tại lẫn nhà cung cấp.
func TestOIDCOnlyAccountDoesNotLeakViaPasswordLogin(t *testing.T) {
	ctx := context.Background()
	svc, repo := newSvc(t)

	u, err := repo.CreateUser(ctx, "google-user@example.com", "G", true)
	if err != nil {
		t.Fatalf("tạo user: %v", err)
	}
	if err := repo.LinkIdentity(ctx, auth.IdentityRecord{
		UserID: u.ID, Provider: auth.ProviderGoogle, Subject: "g-1", Email: u.Email,
	}); err != nil {
		t.Fatalf("liên kết: %v", err)
	}

	_, _, err = svc.SignInWithPassword(ctx, "google-user@example.com", "mat-khau-bat-ky-dai")
	if !errors.Is(err, auth.ErrWrongCredentials) {
		t.Errorf("lỗi = %v, muốn ErrWrongCredentials chung chung", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "google") {
		t.Errorf("thông báo lỗi tiết lộ nhà cung cấp: %v", err)
	}
}

func TestWeakPasswordRejected(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSvc(t)

	if _, _, err := svc.SignUpWithPassword(ctx, "a@example.com", "ngan", "A"); !errors.Is(err, auth.ErrWeakPassword) {
		t.Errorf("lỗi = %v, muốn ErrWeakPassword", err)
	}
}

func TestDuplicateEmailRejected(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSvc(t)

	if _, _, err := svc.SignUpWithPassword(ctx, "a@example.com", "mat-khau-du-dai-12", "A"); err != nil {
		t.Fatalf("đăng ký lần 1: %v", err)
	}
	if _, _, err := svc.SignUpWithPassword(ctx, "a@example.com", "mat-khau-khac-du-dai", "B"); !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("lỗi = %v, muốn ErrEmailTaken", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSvc(t)

	token, _, err := svc.SignUpWithPassword(ctx, "a@example.com", "mat-khau-du-dai-12", "A")
	if err != nil {
		t.Fatalf("đăng ký: %v", err)
	}
	if _, err := svc.Authenticate(ctx, token); err != nil {
		t.Fatalf("token vừa cấp phải dùng được: %v", err)
	}

	if err := svc.SignOut(ctx, token); err != nil {
		t.Fatalf("đăng xuất: %v", err)
	}
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("token sau đăng xuất vẫn dùng được — lỗi = %v", err)
	}
}

// TestSignOutEverywhere là khả năng mà JWT tự ký KHÔNG cho được, và là lý do
// chọn token dạng mờ. Người dùng mất máy phải vô hiệu hoá được mọi phiên ngay.
func TestSignOutEverywhere(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSvc(t)

	t1, user, err := svc.SignUpWithPassword(ctx, "a@example.com", "mat-khau-du-dai-12", "A")
	if err != nil {
		t.Fatalf("đăng ký: %v", err)
	}
	t2, _, err := svc.SignInWithPassword(ctx, "a@example.com", "mat-khau-du-dai-12")
	if err != nil {
		t.Fatalf("đăng nhập thiết bị 2: %v", err)
	}
	if t1 == t2 {
		t.Fatal("hai phiên khác nhau phải có token khác nhau")
	}

	if err := svc.SignOutEverywhere(ctx, user.ID); err != nil {
		t.Fatalf("đăng xuất mọi nơi: %v", err)
	}
	for i, tok := range []string{t1, t2} {
		if _, err := svc.Authenticate(ctx, tok); !errors.Is(err, auth.ErrSessionInvalid) {
			t.Errorf("token %d vẫn dùng được sau khi đăng xuất mọi nơi", i+1)
		}
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	ctx := context.Background()
	repo := memrepo.New(time.Now)
	clock := time.Now()
	svc := auth.NewService(repo, map[auth.Provider]*auth.Verifier{}, func() time.Time { return clock })

	token, _, err := svc.SignUpWithPassword(ctx, "a@example.com", "mat-khau-du-dai-12", "A")
	if err != nil {
		t.Fatalf("đăng ký: %v", err)
	}

	clock = clock.Add(91 * 24 * time.Hour)
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("phiên hết hạn vẫn dùng được — lỗi = %v", err)
	}
}

func TestGarbageTokenRejected(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSvc(t)

	for _, tok := range []string{"", "khong-phai-token", strings.Repeat("A", 64)} {
		if _, err := svc.Authenticate(ctx, tok); !errors.Is(err, auth.ErrSessionInvalid) {
			t.Errorf("token rác %q cho lỗi = %v, muốn ErrSessionInvalid", tok, err)
		}
	}
}
