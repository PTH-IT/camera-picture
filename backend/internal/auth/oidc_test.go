package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Bộ test này là lớp phòng thủ chính của đường xác thực.
//
// Xác minh ID token là chỗ mà một lỗi nhỏ đồng nghĩa với "bất kỳ ai cũng đăng
// nhập được thành bất kỳ ai". Vì vậy phần lớn test dưới đây là test TẤN CÔNG:
// chúng mô phỏng các đòn kinh điển và khẳng định verifier từ chối. Test đường
// thành công chỉ có một, test đường tấn công có mười.

const (
	testIssuer = "https://accounts.google.com"
	testAud    = "client-id-ios.apps.googleusercontent.com"
	testKID    = "test-key-1"
	testNonce  = "nonce-ngau-nhien-tu-client"
	testSub    = "1234567890"
)

type testEnv struct {
	key      *rsa.PrivateKey
	verifier *Verifier
	jwks     *httptest.Server
	fetches  *atomic.Int32
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("sinh khoá: %v", err)
	}

	var fetches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{jwkFor(testKID, &key.PublicKey)},
		})
	}))
	t.Cleanup(srv.Close)

	v := NewVerifier(ProviderGoogle, testIssuer, srv.URL, []string{testAud}, srv.Client())
	return &testEnv{key: key, verifier: v, jwks: srv, fetches: &fetches}
}

func jwkFor(kid string, pub *rsa.PublicKey) map[string]string {
	return map[string]string{
		"kty": "RSA",
		"kid": kid,
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

// claimSet trả về bộ claim hợp lệ, để mỗi test chỉ phải sửa đúng phần nó tấn công.
func claimSet() jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss":            testIssuer,
		"aud":            testAud,
		"sub":            testSub,
		"exp":            now.Add(time.Hour).Unix(),
		"iat":            now.Unix(),
		"nonce":          testNonce,
		"email":          "Nguoi.Dung@Example.com",
		"email_verified": true,
		"name":           "Người Dùng",
	}
}

func (e *testEnv) mint(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = testKID
	s, err := tok.SignedString(e.key)
	if err != nil {
		t.Fatalf("ký token: %v", err)
	}
	return s
}

func TestVerifyValidToken(t *testing.T) {
	e := newTestEnv(t)

	id, err := e.verifier.Verify(context.Background(), e.mint(t, claimSet()), testNonce)
	if err != nil {
		t.Fatalf("token hợp lệ bị từ chối: %v", err)
	}
	if id.Subject != testSub {
		t.Errorf("Subject = %q, muốn %q", id.Subject, testSub)
	}
	// Email phải được chuẩn hoá về chữ thường, nếu không cùng một người đăng nhập
	// hai lần với cách viết hoa khác nhau sẽ thành hai bản ghi.
	if id.Email != "nguoi.dung@example.com" {
		t.Errorf("Email = %q, muốn dạng chữ thường", id.Email)
	}
	if !id.EmailVerified {
		t.Error("EmailVerified = false, muốn true")
	}
	if id.Provider != ProviderGoogle {
		t.Errorf("Provider = %q", id.Provider)
	}
}

// TestRejectAlgNone: đòn kinh điển nhất. Kẻ tấn công tự soạn claim, đặt alg thành
// "none" và bỏ chữ ký. Thư viện nào chấp nhận là mở toang cửa.
func TestRejectAlgNone(t *testing.T) {
	e := newTestEnv(t)

	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claimSet())
	tok.Header["kid"] = testKID
	s, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("tạo token alg=none: %v", err)
	}

	if _, err := e.verifier.Verify(context.Background(), s, testNonce); err == nil {
		t.Fatal("CHẤP NHẬN token alg=none — bất kỳ ai cũng đăng nhập được thành bất kỳ ai")
	}
}

// TestRejectAlgConfusion: kẻ tấn công lấy KHOÁ CÔNG KHAI (ai cũng tải được từ
// JWKS) rồi dùng chính nó làm secret để ký HS256. Nếu verifier không chốt cứng
// thuật toán, nó sẽ lấy khoá công khai ra "xác minh" HMAC và token giả trở thành
// hợp lệ.
func TestRejectAlgConfusion(t *testing.T) {
	e := newTestEnv(t)

	pubDER, err := x509.MarshalPKIXPublicKey(&e.key.PublicKey)
	if err != nil {
		t.Fatalf("mã hoá khoá công khai: %v", err)
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claimSet())
	tok.Header["kid"] = testKID
	s, err := tok.SignedString(pubDER)
	if err != nil {
		t.Fatalf("ký HS256: %v", err)
	}

	if _, err := e.verifier.Verify(context.Background(), s, testNonce); err == nil {
		t.Fatal("CHẤP NHẬN token HS256 ký bằng khoá công khai — lỗi alg confusion")
	}
}

// TestRejectForeignKey: token ký hoàn chỉnh và đúng định dạng, nhưng bằng khoá
// của kẻ tấn công. Chỉ chữ ký khớp JWKS mới được chấp nhận.
func TestRejectForeignKey(t *testing.T) {
	e := newTestEnv(t)

	attacker, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("sinh khoá kẻ tấn công: %v", err)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claimSet())
	tok.Header["kid"] = testKID // giả mạo kid của nhà cung cấp thật
	s, err := tok.SignedString(attacker)
	if err != nil {
		t.Fatalf("ký: %v", err)
	}

	if _, err := e.verifier.Verify(context.Background(), s, testNonce); err == nil {
		t.Fatal("CHẤP NHẬN token ký bằng khoá lạ")
	}
}

// TestRejectTamperedPayload: giữ nguyên chữ ký hợp lệ, chỉ sửa phần payload.
func TestRejectTamperedPayload(t *testing.T) {
	e := newTestEnv(t)
	valid := e.mint(t, claimSet())

	parts := strings.Split(valid, ".")
	if len(parts) != 3 {
		t.Fatalf("token không có 3 phần")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("giải mã payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("giải mã claim: %v", err)
	}
	claims["sub"] = "nan-nhan-999" // đổi sang danh tính người khác
	tampered, _ := json.Marshal(claims)
	parts[1] = base64.RawURLEncoding.EncodeToString(tampered)

	if _, err := e.verifier.Verify(context.Background(), strings.Join(parts, "."), testNonce); err == nil {
		t.Fatal("CHẤP NHẬN token đã bị sửa payload")
	}
}

func TestRejectExpired(t *testing.T) {
	e := newTestEnv(t)
	c := claimSet()
	// Quá hạn xa hơn khoảng leeway 30 giây.
	c["exp"] = time.Now().Add(-10 * time.Minute).Unix()

	_, err := e.verifier.Verify(context.Background(), e.mint(t, c), testNonce)
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("lỗi = %v, muốn ErrTokenExpired", err)
	}
}

func TestRejectWrongAudience(t *testing.T) {
	e := newTestEnv(t)
	c := claimSet()
	// Token thật, do Google cấp, nhưng cho MỘT APP KHÁC. Không kiểm aud thì bất
	// kỳ app nào cũng lấy token của người dùng rồi đăng nhập vào app của ta.
	c["aud"] = "app-khac.apps.googleusercontent.com"

	if _, err := e.verifier.Verify(context.Background(), e.mint(t, c), testNonce); err == nil {
		t.Fatal("CHẤP NHẬN token cấp cho app khác")
	}
}

func TestRejectWrongIssuer(t *testing.T) {
	e := newTestEnv(t)
	c := claimSet()
	c["iss"] = "https://ke-tan-cong.example.com"

	if _, err := e.verifier.Verify(context.Background(), e.mint(t, c), testNonce); err == nil {
		t.Fatal("CHẤP NHẬN token có issuer sai")
	}
}

// TestRejectNonceMismatch: token thật, chưa hết hạn, đúng aud — nhưng bị chặn
// lại từ một phiên đăng nhập khác và phát lại. Nonce là thứ duy nhất phân biệt.
func TestRejectNonceMismatch(t *testing.T) {
	e := newTestEnv(t)

	_, err := e.verifier.Verify(context.Background(), e.mint(t, claimSet()), "nonce-cua-phien-khac")
	if !errors.Is(err, ErrNonceMismatch) {
		t.Errorf("lỗi = %v, muốn ErrNonceMismatch", err)
	}
}

// TestRequireNonce: gọi Verify với nonce rỗng phải bị từ chối ngay, kể cả khi
// token hoàn toàn hợp lệ. Nếu cho phép, lập trình viên sẽ vô tình tắt lớp chống
// phát lại chỉ bằng cách quên truyền tham số.
func TestRequireNonce(t *testing.T) {
	e := newTestEnv(t)

	if _, err := e.verifier.Verify(context.Background(), e.mint(t, claimSet()), ""); err == nil {
		t.Fatal("CHẤP NHẬN xác minh không có nonce")
	}
}

func TestRejectMissingSubject(t *testing.T) {
	e := newTestEnv(t)
	c := claimSet()
	delete(c, "sub")

	if _, err := e.verifier.Verify(context.Background(), e.mint(t, c), testNonce); err == nil {
		t.Fatal("CHẤP NHẬN token không có sub")
	}
}

func TestRejectUnknownKID(t *testing.T) {
	e := newTestEnv(t)

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claimSet())
	tok.Header["kid"] = "kid-khong-ton-tai"
	s, _ := tok.SignedString(e.key)

	if _, err := e.verifier.Verify(context.Background(), s, testNonce); err == nil {
		t.Fatal("CHẤP NHẬN token có kid không nằm trong JWKS")
	}
}

// TestUnknownKIDDoesNotHammerProvider: không có giới hạn, bất kỳ ai cũng biến
// server của ta thành công cụ dội request vào Google chỉ bằng cách gửi token rác
// liên tục.
func TestUnknownKIDDoesNotHammerProvider(t *testing.T) {
	e := newTestEnv(t)

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claimSet())
	tok.Header["kid"] = "kid-rac"
	s, _ := tok.SignedString(e.key)

	for i := 0; i < 20; i++ {
		_, _ = e.verifier.Verify(context.Background(), s, testNonce)
	}

	if n := e.fetches.Load(); n > 2 {
		t.Errorf("gọi JWKS %d lần cho 20 token rác — thiếu giới hạn tần suất", n)
	}
	t.Logf("20 token rác chỉ gây %d lần gọi JWKS", e.fetches.Load())
}

// TestAppleStringEmailVerified: Apple trả email_verified dưới dạng CHUỖI "true"
// trong một số trường hợp còn Google trả boolean. Không xử lý cả hai thì toàn bộ
// đăng nhập Apple hỏng ở bước giải mã, với thông báo lỗi không liên quan gì tới
// nguyên nhân.
func TestAppleStringEmailVerified(t *testing.T) {
	e := newTestEnv(t)
	c := claimSet()
	c["email_verified"] = "true"

	id, err := e.verifier.Verify(context.Background(), e.mint(t, c), testNonce)
	if err != nil {
		t.Fatalf("từ chối token có email_verified dạng chuỗi: %v", err)
	}
	if !id.EmailVerified {
		t.Error(`email_verified = "true" (chuỗi) phải cho ra true`)
	}
}

// TestRejectWeakKeyInJWKS: nếu JWKS bị can thiệp và cấp khoá RSA ngắn, kẻ tấn
// công có thể bẻ khoá đó rồi ký token tuỳ ý.
func TestRejectWeakKeyInJWKS(t *testing.T) {
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("sinh khoá yếu: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{jwkFor(testKID, &weak.PublicKey)},
		})
	}))
	defer srv.Close()

	v := NewVerifier(ProviderGoogle, testIssuer, srv.URL, []string{testAud}, srv.Client())

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claimSet())
	tok.Header["kid"] = testKID
	s, _ := tok.SignedString(weak)

	if _, err := v.Verify(context.Background(), s, testNonce); err == nil {
		t.Fatal("CHẤP NHẬN token ký bằng khoá RSA 1024 bit")
	}
}

func TestJWKSCacheAvoidsRefetch(t *testing.T) {
	e := newTestEnv(t)
	tokStr := e.mint(t, claimSet())

	for i := 0; i < 10; i++ {
		if _, err := e.verifier.Verify(context.Background(), tokStr, testNonce); err != nil {
			t.Fatalf("lần %d: %v", i, err)
		}
	}
	if n := e.fetches.Load(); n != 1 {
		t.Errorf("gọi JWKS %d lần cho 10 lần xác minh, muốn 1", n)
	}
}
