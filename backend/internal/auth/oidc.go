// Package auth xác thực người dùng qua Sign in with Apple, Google Sign-In và
// email + mật khẩu.
//
// Nguyên tắc bao trùm: TOKEN LUÔN ĐƯỢC XÁC MINH PHÍA SERVER. Client gửi ID token
// và server tự lấy JWKS của nhà cung cấp để kiểm chữ ký. Không bao giờ tin danh
// tính do client tự khai — một client bị sửa đổi có thể khai là bất kỳ ai.
//
// Xem docs/adr/0002-auth-and-storage.md về lý do phải có cả ba phương thức.
package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Endpoint JWKS của các nhà cung cấp.
const (
	GoogleIssuer  = "https://accounts.google.com"
	GoogleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

	AppleIssuer  = "https://appleid.apple.com"
	AppleJWKSURL = "https://appleid.apple.com/auth/keys"
)

type Provider string

const (
	ProviderApple    Provider = "apple"
	ProviderGoogle   Provider = "google"
	ProviderPassword Provider = "password"
)

// Identity là danh tính đã được xác minh từ một nhà cung cấp.
type Identity struct {
	Provider Provider
	// Subject là khoá định danh ỔN ĐỊNH. Dùng nó, đừng dùng email.
	//
	// Apple cấp email relay dạng @privaterelay.appleid.com và người dùng có thể
	// đổi hoặc tắt chuyển tiếp; Google cho phép đổi email chính. Lấy email làm
	// khoá là cách chắc chắn để một ngày nào đó ghép nhầm hai người vào một tài
	// khoản, hoặc khoá một người ra khỏi tài khoản của chính họ.
	Subject       string
	Email         string
	EmailVerified bool
	// Name chỉ có ở lần uỷ quyền ĐẦU TIÊN với Apple — xem chú thích của
	// AppleFirstAuthName.
	Name string
}

var (
	ErrInvalidToken  = errors.New("token không hợp lệ")
	ErrTokenExpired  = errors.New("token đã hết hạn")
	ErrNonceMismatch = errors.New("nonce không khớp")
)

// Verifier xác minh ID token của một nhà cung cấp OIDC.
type Verifier struct {
	provider Provider
	issuer   string
	// audiences là danh sách client id hợp lệ. Nhiều hơn một vì iOS, Android và
	// web thường có client id riêng.
	audiences []string
	keys      *jwksCache
}

func NewVerifier(provider Provider, issuer, jwksURL string, audiences []string, httpClient *http.Client) *Verifier {
	return &Verifier{
		provider:  provider,
		issuer:    issuer,
		audiences: audiences,
		keys:      newJWKSCache(jwksURL, httpClient),
	}
}

func NewGoogleVerifier(audiences []string, c *http.Client) *Verifier {
	return NewVerifier(ProviderGoogle, GoogleIssuer, GoogleJWKSURL, audiences, c)
}

func NewAppleVerifier(audiences []string, c *http.Client) *Verifier {
	return NewVerifier(ProviderApple, AppleIssuer, AppleJWKSURL, audiences, c)
}

// Verify kiểm tra chữ ký và các claim của ID token.
//
// expectedNonce KHÔNG được rỗng. Không có nonce thì một ID token hợp lệ bị chặn
// được có thể phát lại để đăng nhập dưới danh nghĩa nạn nhân — client gửi lại
// đúng token đó và server không có cách nào phân biệt. Client phải sinh nonce
// ngẫu nhiên cho mỗi lần đăng nhập và gửi kèm.
func (v *Verifier) Verify(ctx context.Context, idToken, expectedNonce string) (Identity, error) {
	if expectedNonce == "" {
		return Identity{}, fmt.Errorf("%w: thiếu nonce", ErrInvalidToken)
	}

	parser := jwt.NewParser(
		// Chốt cứng thuật toán. Đây là phòng thủ chống ALG CONFUSION: nếu chấp
		// nhận danh sách mở, kẻ tấn công có thể gửi token ký bằng HS256 với khoá
		// công khai RSA làm secret — khoá đó công khai nên ai cũng ký được — và
		// thư viện sẽ coi là hợp lệ. Cả Google lẫn Apple đều chỉ dùng RS256.
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
		// Cho phép lệch đồng hồ nhỏ giữa server của ta và server nhà cung cấp.
		jwt.WithLeeway(30*time.Second),
	)

	var claims idTokenClaims
	_, err := parser.ParseWithClaims(idToken, &claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("%w: thiếu kid trong header", ErrInvalidToken)
		}
		return v.keys.key(ctx, kid)
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Identity{}, ErrTokenExpired
		}
		return Identity{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	// Kiểm aud thủ công thay vì dùng jwt.WithAudience: thư viện chỉ nhận một
	// audience, còn ta cần chấp nhận nhiều client id (iOS, Android, web).
	if !containsString(claims.Audience, v.audiences) {
		return Identity{}, fmt.Errorf("%w: aud không nằm trong danh sách cho phép", ErrInvalidToken)
	}

	// So sánh nonce. Nếu token không có nonce mà ta lại yêu cầu, đó là token
	// được lấy từ luồng khác — từ chối.
	if claims.Nonce != expectedNonce {
		return Identity{}, ErrNonceMismatch
	}

	if claims.Subject == "" {
		return Identity{}, fmt.Errorf("%w: thiếu sub", ErrInvalidToken)
	}

	return Identity{
		Provider:      v.provider,
		Subject:       claims.Subject,
		Email:         strings.ToLower(claims.Email),
		EmailVerified: claims.EmailVerified.Bool(),
		Name:          claims.Name,
	}, nil
}

type idTokenClaims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
	// Apple trả email_verified dưới dạng CHUỖI "true" trong một số trường hợp,
	// Google trả boolean. Kiểu tuỳ biến bên dưới nuốt cả hai — không có nó,
	// giải mã token của Apple sẽ lỗi và toàn bộ đăng nhập Apple hỏng.
	EmailVerified flexBool `json:"email_verified"`
	Nonce         string   `json:"nonce"`
	Name          string   `json:"name"`
}

type flexBool struct{ v bool }

func (f flexBool) Bool() bool { return f.v }

func (f *flexBool) UnmarshalJSON(b []byte) error {
	var asBool bool
	if err := json.Unmarshal(b, &asBool); err == nil {
		f.v = asBool
		return nil
	}
	var asString string
	if err := json.Unmarshal(b, &asString); err == nil {
		f.v = asString == "true"
		return nil
	}
	return fmt.Errorf("email_verified không phải bool cũng không phải chuỗi")
}

func containsString(got jwt.ClaimStrings, want []string) bool {
	for _, g := range got {
		for _, w := range want {
			if g == w {
				return true
			}
		}
	}
	return false
}

// jwksCache lấy và cache khoá công khai của nhà cung cấp.
type jwksCache struct {
	url    string
	client *http.Client

	mu        sync.RWMutex
	keys      map[string]any
	fetchedAt time.Time
	// lastMiss chống việc một token có kid rác kéo theo một lần gọi mạng. Không
	// có nó, bất kỳ ai cũng có thể biến server của ta thành công cụ dội request
	// vào Google chỉ bằng cách gửi token rác liên tục.
	lastMiss time.Time
}

const (
	jwksTTL       = 1 * time.Hour
	jwksMissDelay = 1 * time.Minute
)

func newJWKSCache(url string, client *http.Client) *jwksCache {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &jwksCache{url: url, client: client, keys: map[string]any{}}
}

func (c *jwksCache) key(ctx context.Context, kid string) (any, error) {
	c.mu.RLock()
	k, ok := c.keys[kid]
	fresh := time.Since(c.fetchedAt) < jwksTTL
	recentMiss := time.Since(c.lastMiss) < jwksMissDelay
	c.mu.RUnlock()

	if ok && fresh {
		return k, nil
	}
	if !ok && recentMiss && fresh {
		return nil, fmt.Errorf("%w: kid không xác định", ErrInvalidToken)
	}

	if err := c.refresh(ctx); err != nil {
		// Khoá cũ còn dùng được thì vẫn dùng: nhà cung cấp xoay khoá không
		// thường xuyên, và để cả hệ thống đăng nhập chết vì một lần gọi mạng
		// hỏng là đánh đổi tồi.
		if ok {
			return k, nil
		}
		return nil, err
	}

	c.mu.RLock()
	k, ok = c.keys[kid]
	c.mu.RUnlock()
	if !ok {
		c.mu.Lock()
		c.lastMiss = time.Now()
		c.mu.Unlock()
		return nil, fmt.Errorf("%w: kid không xác định", ErrInvalidToken)
	}
	return k, nil
}

func (c *jwksCache) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("lấy JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("lấy JWKS: mã trạng thái %d", resp.StatusCode)
	}

	var set struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return fmt.Errorf("giải mã JWKS: %w", err)
	}

	parsed := make(map[string]any, len(set.Keys))
	for _, raw := range set.Keys {
		var hdr struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
		}
		if err := json.Unmarshal(raw, &hdr); err != nil || hdr.Kid == "" {
			continue
		}
		// Chỉ nhận RSA. Google và Apple đều dùng RS256; chấp nhận loại khoá khác
		// là mở rộng bề mặt tấn công mà không được gì.
		if hdr.Kty != "RSA" {
			continue
		}
		key, err := parseRSAJWK(raw)
		if err != nil {
			continue
		}
		parsed[hdr.Kid] = key
	}
	if len(parsed) == 0 {
		return fmt.Errorf("JWKS không chứa khoá RSA nào dùng được")
	}

	c.mu.Lock()
	c.keys = parsed
	c.fetchedAt = time.Now()
	c.mu.Unlock()
	return nil
}

// parseRSAJWK dựng khoá công khai RSA từ một JWK.
//
// golang-jwt không có hàm này (chỉ đọc được PEM), và JWK RSA thì đơn giản: modulus
// và số mũ ở dạng base64url. Tự dựng ở đây rẻ hơn là thêm một dependency nữa vào
// đường xác thực — nơi mà mỗi dependency đều là bề mặt tấn công.
func parseRSAJWK(raw json.RawMessage) (*rsa.PublicKey, error) {
	var jwk struct {
		Kty string `json:"kty"`
		N   string `json:"n"`
		E   string `json:"e"`
	}
	if err := json.Unmarshal(raw, &jwk); err != nil {
		return nil, err
	}
	if jwk.Kty != "RSA" {
		return nil, fmt.Errorf("kty = %q, cần RSA", jwk.Kty)
	}

	nb, err := decodeBase64URL(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("modulus: %w", err)
	}
	eb, err := decodeBase64URL(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("số mũ: %w", err)
	}

	e := new(big.Int).SetBytes(eb)
	// Số mũ công khai phải vừa trong int và hợp lệ. Thực tế luôn là 65537;
	// kiểm tra để một JWKS bị can thiệp không gây tràn số.
	if !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 {
		return nil, fmt.Errorf("số mũ công khai ngoài khoảng hợp lệ")
	}

	n := new(big.Int).SetBytes(nb)
	// Từ chối khoá quá ngắn. RSA dưới 2048 bit không còn được coi là an toàn, và
	// một JWKS giả mạo có thể cấp khoá 512 bit để bẻ được trong thời gian ngắn.
	if n.BitLen() < 2048 {
		return nil, fmt.Errorf("khoá RSA %d bit quá ngắn", n.BitLen())
	}

	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}

// decodeBase64URL nhận cả dạng có padding lẫn không, vì các nhà cung cấp không
// thống nhất với nhau.
func decodeBase64URL(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	return base64.RawURLEncoding.DecodeString(s)
}
