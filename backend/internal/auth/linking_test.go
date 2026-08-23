package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hauph/camera/backend/internal/auth"
	"github.com/hauph/camera/backend/internal/auth/memrepo"
)

// Bộ test này bảo vệ quyết định bảo mật quan trọng nhất của tầng tài khoản:
// KHI NÀO được tự động ghép một danh tính mới vào tài khoản đã có.
//
// Ghép sai là chiếm được tài khoản người khác. Kịch bản cụ thể được mô phỏng ở
// TestAttackerCannotHijackViaUnverifiedEmail.

const (
	linkIssuer = "https://accounts.google.com"
	linkAud    = "client-id-ios"
	linkKID    = "k1"
	linkNonce  = "nonce-tu-client"
)

type linkEnv struct {
	svc  *auth.Service
	repo *memrepo.Repo
	key  *rsa.PrivateKey
}

func newLinkEnv(t *testing.T) *linkEnv {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("sinh khoá: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": linkKID, "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)

	repo := memrepo.New(time.Now)
	v := auth.NewVerifier(auth.ProviderGoogle, linkIssuer, srv.URL, []string{linkAud}, srv.Client())
	svc := auth.NewService(repo, map[auth.Provider]*auth.Verifier{auth.ProviderGoogle: v}, time.Now)

	return &linkEnv{svc: svc, repo: repo, key: key}
}

func (e *linkEnv) mint(t *testing.T, sub, email string, verified bool) string {
	t.Helper()
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": linkIssuer, "aud": linkAud, "sub": sub,
		"exp": now.Add(time.Hour).Unix(), "iat": now.Unix(),
		"nonce": linkNonce, "email": email, "email_verified": verified,
	})
	tok.Header["kid"] = linkKID
	s, err := tok.SignedString(e.key)
	if err != nil {
		t.Fatalf("ký token: %v", err)
	}
	return s
}

// TestAttackerCannotHijackViaUnverifiedEmail mô phỏng đúng kịch bản tấn công mà
// quy tắc ghép tài khoản tồn tại để chặn:
//
//  1. Kẻ tấn công đăng ký tài khoản MẬT KHẨU bằng email của nạn nhân. Không cần
//     truy cập hòm thư — chỉ cần biết địa chỉ. Email này CHƯA XÁC MINH.
//  2. Sau đó nạn nhân đăng nhập bằng Google với chính email đó.
//  3. Nếu hệ thống tự động ghép theo email, nạn nhân sẽ mang toàn bộ dữ liệu thật
//     vào một tài khoản mà kẻ tấn công đang giữ mật khẩu.
//
// Đây là lỗi rất phổ biến và rất khó phát hiện sau khi đã xảy ra.
func TestAttackerCannotHijackViaUnverifiedEmail(t *testing.T) {
	ctx := context.Background()
	e := newLinkEnv(t)
	const victimEmail = "nan-nhan@example.com"

	// Bước 1: kẻ tấn công chiếm chỗ email bằng đăng ký mật khẩu.
	_, attacker, err := e.svc.SignUpWithPassword(ctx, victimEmail, "mat-khau-ke-tan-cong", "Kẻ tấn công")
	if err != nil {
		t.Fatalf("đăng ký của kẻ tấn công: %v", err)
	}

	// Bước 2: nạn nhân đăng nhập bằng Google, email ĐÃ xác minh.
	_, _, err = e.svc.SignInWithOIDC(ctx, auth.ProviderGoogle,
		e.mint(t, "google-nan-nhan", victimEmail, true), linkNonce, "")

	// Bước 3: phải bị chặn.
	if err == nil {
		t.Fatal("TỰ ĐỘNG GHÉP vào tài khoản chưa xác minh — kẻ tấn công chiếm được tài khoản nạn nhân")
	}
	if !errors.Is(err, auth.ErrLinkRequiresSignIn) {
		t.Fatalf("lỗi = %v, muốn ErrLinkRequiresSignIn", err)
	}

	// Và tài khoản của kẻ tấn công không được nhận thêm danh tính nào.
	ids, err := e.repo.IdentitiesOf(ctx, attacker.ID)
	if err != nil {
		t.Fatalf("IdentitiesOf: %v", err)
	}
	for _, id := range ids {
		if id.Provider == auth.ProviderGoogle {
			t.Error("danh tính Google đã bị gắn vào tài khoản của kẻ tấn công")
		}
	}
}

// TestAutoLinkWhenBothVerified: khi cả hai phía đều có email đã xác minh, ghép tự
// động là an toàn và đúng mong đợi — người dùng không nên bị buộc tạo hai tài
// khoản chỉ vì đăng nhập bằng cách khác.
func TestAutoLinkWhenBothVerified(t *testing.T) {
	ctx := context.Background()
	e := newLinkEnv(t)
	const email = "nguoi-dung@example.com"

	// Tài khoản có sẵn với email ĐÃ xác minh (ví dụ tạo từ lần đăng nhập Apple trước).
	existing, err := e.repo.CreateUser(ctx, email, "Người Dùng", true)
	if err != nil {
		t.Fatalf("tạo user: %v", err)
	}
	if err := e.repo.LinkIdentity(ctx, auth.IdentityRecord{
		UserID: existing.ID, Provider: auth.ProviderApple, Subject: "apple-1", Email: email,
	}); err != nil {
		t.Fatalf("liên kết Apple: %v", err)
	}

	_, got, err := e.svc.SignInWithOIDC(ctx, auth.ProviderGoogle,
		e.mint(t, "google-1", email, true), linkNonce, "")
	if err != nil {
		t.Fatalf("đăng nhập Google: %v", err)
	}
	if got.ID != existing.ID {
		t.Errorf("tạo tài khoản mới (%q) thay vì ghép vào tài khoản có sẵn (%q)", got.ID, existing.ID)
	}
}

// TestSubjectIsTheKeyNotEmail: người dùng đổi email ở phía Google. Lần đăng nhập
// sau phải ra ĐÚNG tài khoản cũ, vì khoá tra cứu là sub chứ không phải email.
//
// Dùng email làm khoá là cách chắc chắn để một ngày nào đó khoá người dùng ra
// khỏi tài khoản của chính họ.
func TestSubjectIsTheKeyNotEmail(t *testing.T) {
	ctx := context.Background()
	e := newLinkEnv(t)

	_, first, err := e.svc.SignInWithOIDC(ctx, auth.ProviderGoogle,
		e.mint(t, "sub-on-dinh", "cu@example.com", true), linkNonce, "")
	if err != nil {
		t.Fatalf("đăng nhập lần 1: %v", err)
	}

	// Cùng sub, email đã đổi.
	_, second, err := e.svc.SignInWithOIDC(ctx, auth.ProviderGoogle,
		e.mint(t, "sub-on-dinh", "moi@example.com", true), linkNonce, "")
	if err != nil {
		t.Fatalf("đăng nhập lần 2: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("đổi email tạo ra tài khoản mới (%q vs %q) — khoá tra cứu đang sai",
			second.ID, first.ID)
	}
}

// TestAppleFirstAuthNameIsCaptured: Apple CHỈ trả tên ở lần uỷ quyền đầu tiên, và
// trả cho client chứ không nằm trong ID token. Không lưu ngay là mất vĩnh viễn —
// không có API nào lấy lại được.
func TestAppleFirstAuthNameIsCaptured(t *testing.T) {
	ctx := context.Background()
	e := newLinkEnv(t)

	// ID token không chứa name; client chuyển tiếp riêng.
	_, user, err := e.svc.SignInWithOIDC(ctx, auth.ProviderGoogle,
		e.mint(t, "sub-1", "a@example.com", true), linkNonce, "Trần Văn A")
	if err != nil {
		t.Fatalf("đăng nhập: %v", err)
	}
	if user.Name != "Trần Văn A" {
		t.Errorf("Name = %q, muốn %q — tên lần đầu bị mất", user.Name, "Trần Văn A")
	}

	// Lần sau không có tên: KHÔNG được ghi đè thành rỗng.
	_, again, err := e.svc.SignInWithOIDC(ctx, auth.ProviderGoogle,
		e.mint(t, "sub-1", "a@example.com", true), linkNonce, "")
	if err != nil {
		t.Fatalf("đăng nhập lần 2: %v", err)
	}
	if again.Name != "Trần Văn A" {
		t.Errorf("Name = %q sau lần đăng nhập thứ hai — tên đã bị xoá mất", again.Name)
	}
}

// TestHiddenEmailUserGetsAccount: người dùng Apple có thể ẩn email hoàn toàn.
// Không được coi đó là lỗi — họ vẫn phải có tài khoản dùng được.
func TestHiddenEmailUserGetsAccount(t *testing.T) {
	ctx := context.Background()
	e := newLinkEnv(t)

	_, user, err := e.svc.SignInWithOIDC(ctx, auth.ProviderGoogle,
		e.mint(t, "sub-an-danh", "", false), linkNonce, "")
	if err != nil {
		t.Fatalf("người dùng ẩn email bị từ chối: %v", err)
	}
	if user.ID == "" {
		t.Error("không tạo được tài khoản cho người dùng ẩn email")
	}

	// Người ẩn email thứ hai KHÔNG được ghép nhầm vào tài khoản người thứ nhất
	// chỉ vì cả hai đều có email rỗng.
	_, other, err := e.svc.SignInWithOIDC(ctx, auth.ProviderGoogle,
		e.mint(t, "sub-an-danh-2", "", false), linkNonce, "")
	if err != nil {
		t.Fatalf("người dùng ẩn email thứ hai: %v", err)
	}
	if other.ID == user.ID {
		t.Error("hai người dùng ẩn email bị gộp vào cùng một tài khoản")
	}
}
