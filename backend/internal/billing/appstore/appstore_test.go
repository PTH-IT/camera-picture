package appstore

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Bộ test này dựng một "Apple giả" hoàn chỉnh: chứng thư gốc, trung gian, và lá,
// rồi ký JWS đúng như Apple làm.
//
// Phần lớn test là test TẤN CÔNG. Lý do: chuỗi chứng thư nằm NGAY TRONG header
// x5c của chính JWS cần kiểm. Kẻ tấn công tự dựng được chuỗi của hắn và ký một
// JWS trông hoàn toàn hợp lệ. Thứ duy nhất phân biệt thật giả là chuỗi đó có bắt
// nguồn từ chứng thư gốc ta tự cấu hình hay không — và test quan trọng nhất ở
// đây chính là test khẳng định điều đó.

const testBundleID = "vn.pth.camera-picture"

type chain struct {
	rootPEM []byte
	certs   []*x509.Certificate // leaf, intermediate, root
	leafKey *ecdsa.PrivateKey
}

// newChain dựng chuỗi root -> intermediate -> leaf.
func newChain(t *testing.T, notBefore, notAfter time.Time) *chain {
	t.Helper()

	rootKey, rootCert := makeCA(t, "Test Apple Root CA", nil, nil, notBefore, notAfter)
	interKey, interCert := makeCA(t, "Test Apple Intermediate", rootCert, rootKey, notBefore, notAfter)
	leafKey, leafCert := makeLeaf(t, "Test Apple Leaf", interCert, interKey, notBefore, notAfter)

	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootCert.Raw})
	return &chain{
		rootPEM: rootPEM,
		certs:   []*x509.Certificate{leafCert, interCert, rootCert},
		leafKey: leafKey,
	}
}

func makeCA(t *testing.T, cn string, parent *x509.Certificate, parentKey *ecdsa.PrivateKey,
	notBefore, notAfter time.Time) (*ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("sinh khoá: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(t),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	signer, signerKey := tmpl, key
	if parent != nil {
		signer, signerKey = parent, parentKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatalf("tạo chứng thư CA: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("đọc chứng thư CA: %v", err)
	}
	return key, cert
}

func makeLeaf(t *testing.T, cn string, parent *x509.Certificate, parentKey *ecdsa.PrivateKey,
	notBefore, notAfter time.Time) (*ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("sinh khoá: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(t),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatalf("tạo chứng thư lá: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("đọc chứng thư lá: %v", err)
	}
	return key, cert
}

func serial(t *testing.T) *big.Int {
	t.Helper()
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("sinh serial: %v", err)
	}
	return n
}

// sign ký payload bằng khoá lá và đính chuỗi x5c, đúng như Apple làm.
func (c *chain) sign(t *testing.T, payload map[string]any) string {
	t.Helper()

	x5c := make([]string, len(c.certs))
	for i, cert := range c.certs {
		x5c[i] = base64.StdEncoding.EncodeToString(cert.Raw)
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims(payload))
	tok.Header["x5c"] = x5c

	s, err := tok.SignedString(c.leafKey)
	if err != nil {
		t.Fatalf("ký JWS: %v", err)
	}
	return s
}

func txPayload() map[string]any {
	now := time.Now()
	return map[string]any{
		"bundleId":              testBundleID,
		"productId":             "storage_1tb_monthly",
		"originalTransactionId": "2000000123456789",
		"transactionId":         "2000000987654321",
		"expiresDate":           now.Add(30 * 24 * time.Hour).UnixMilli(),
		"purchaseDate":          now.UnixMilli(),
		"type":                  "Auto-Renewable Subscription",
		"environment":           "Production",
	}
}

func newVerifier(t *testing.T, c *chain) *Verifier {
	t.Helper()
	v, err := New(Config{
		AppleRootCertsPEM: c.rootPEM,
		BundleID:          testBundleID,
		Environment:       "Production",
		Now:               time.Now,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return v
}

func TestVerifyValidTransaction(t *testing.T) {
	c := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	v := newVerifier(t, c)

	p, err := v.VerifyTransaction(c.sign(t, txPayload()))
	if err != nil {
		t.Fatalf("giao dịch hợp lệ bị từ chối: %v", err)
	}
	if p.ProductID != "storage_1tb_monthly" {
		t.Errorf("ProductID = %q", p.ProductID)
	}
	if p.Revoked {
		t.Error("Revoked = true với giao dịch bình thường")
	}
	if p.ExpiresAt.IsZero() {
		t.Error("ExpiresAt rỗng")
	}
}

// TestUsesOriginalTransactionID canh giữ một lỗi rất dễ mắc: Apple trả CẢ HAI
// trường transactionId và originalTransactionId, và cả hai đều trông hợp lý.
//
// transactionId ĐỔI mỗi lần gia hạn. Dùng nó làm khoá thì mỗi tháng tạo ra một
// quyền lợi MỚI thay vì cập nhật cái cũ, và dung lượng người dùng tăng vô hạn
// theo thời gian — một lỗi tốn tiền mà không ai báo vì nó có lợi cho người dùng.
func TestUsesOriginalTransactionID(t *testing.T) {
	c := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	v := newVerifier(t, c)

	p, err := v.VerifyTransaction(c.sign(t, txPayload()))
	if err != nil {
		t.Fatalf("VerifyTransaction: %v", err)
	}
	if p.TransactionID != "2000000123456789" {
		t.Errorf("TransactionID = %q, muốn originalTransactionId (2000000123456789), "+
			"không phải transactionId — dùng sai sẽ khiến quyền lợi nhân lên mỗi lần gia hạn",
			p.TransactionID)
	}
}

// TestRejectsForeignCertChain là test QUAN TRỌNG NHẤT của package.
//
// Kẻ tấn công dựng chuỗi chứng thư của chính hắn, ký một payload bịa đặt, và
// đính chuỗi đó vào x5c. Chữ ký KHỚP hoàn hảo với chuỗi đi kèm — mọi phép kiểm
// tra chữ ký đơn thuần đều nói "hợp lệ".
//
// Thứ duy nhất chặn được là kiểm tra chuỗi có bắt nguồn từ chứng thư gốc của
// Apple mà ta tự cấu hình hay không.
func TestRejectsForeignCertChain(t *testing.T) {
	real := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	v := newVerifier(t, real)

	// Chuỗi hoàn toàn hợp lệ về mặt mật mã — chỉ là của người khác.
	attacker := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	forged := txPayload()
	forged["productId"] = "storage_2tb_monthly"
	forged["originalTransactionId"] = "tu-che-999"

	_, err := v.VerifyTransaction(attacker.sign(t, forged))
	if err == nil {
		t.Fatal("CHẤP NHẬN JWS ký bằng chuỗi chứng thư tự dựng — mọi hoá đơn tự chế đều qua được")
	}
	if !errors.Is(err, ErrInvalidSignature) && !errors.Is(err, ErrUntrustedChain) {
		t.Errorf("lỗi = %v, muốn ErrUntrustedChain", err)
	}
}

func TestRejectsMissingCertChain(t *testing.T) {
	c := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	v := newVerifier(t, c)

	// Ký đúng khoá lá nhưng KHÔNG đính x5c.
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims(txPayload()))
	s, err := tok.SignedString(c.leafKey)
	if err != nil {
		t.Fatalf("ký: %v", err)
	}

	if _, err := v.VerifyTransaction(s); err == nil {
		t.Fatal("CHẤP NHẬN JWS không có chuỗi chứng thư")
	}
}

// TestRejectsExpiredLeafCert: chứng thư hết hạn phải bị từ chối, nếu không thì
// một chứng thư Apple cũ bị lộ vẫn dùng được vĩnh viễn.
func TestRejectsExpiredLeafCert(t *testing.T) {
	past := time.Now().Add(-48 * time.Hour)
	c := newChain(t, past, past.Add(time.Hour)) // hết hạn 47 giờ trước
	v := newVerifier(t, c)

	if _, err := v.VerifyTransaction(c.sign(t, txPayload())); err == nil {
		t.Fatal("CHẤP NHẬN JWS ký bằng chứng thư đã hết hạn")
	}
}

// TestRejectsAlgConfusion: chốt cứng ES256. Không chốt thì kẻ tấn công gửi token
// HS256 ký bằng chính khoá công khai lấy từ x5c — khoá đó công khai nên ai cũng
// ký được — và thư viện sẽ coi là hợp lệ.
func TestRejectsAlgConfusion(t *testing.T) {
	c := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	v := newVerifier(t, c)

	x5c := make([]string, len(c.certs))
	for i, cert := range c.certs {
		x5c[i] = base64.StdEncoding.EncodeToString(cert.Raw)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&c.leafKey.PublicKey)
	if err != nil {
		t.Fatalf("mã hoá khoá công khai: %v", err)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(txPayload()))
	tok.Header["x5c"] = x5c
	s, err := tok.SignedString(pubDER)
	if err != nil {
		t.Fatalf("ký HS256: %v", err)
	}

	if _, err := v.VerifyTransaction(s); err == nil {
		t.Fatal("CHẤP NHẬN token HS256 ký bằng khoá công khai — lỗi alg confusion")
	}
}

func TestRejectsAlgNone(t *testing.T) {
	c := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	v := newVerifier(t, c)

	tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims(txPayload()))
	s, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("tạo token alg=none: %v", err)
	}
	if _, err := v.VerifyTransaction(s); err == nil {
		t.Fatal("CHẤP NHẬN token alg=none")
	}
}

func TestRejectsTamperedPayload(t *testing.T) {
	c := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	v := newVerifier(t, c)

	valid := c.sign(t, txPayload())
	parts := strings.Split(valid, ".")
	if len(parts) != 3 {
		t.Fatal("JWS không có 3 phần")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("giải mã payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("giải mã claim: %v", err)
	}
	claims["productId"] = "storage_2tb_monthly" // nâng cấp gói miễn phí
	tampered, _ := json.Marshal(claims)
	parts[1] = base64.RawURLEncoding.EncodeToString(tampered)

	if _, err := v.VerifyTransaction(strings.Join(parts, ".")); err == nil {
		t.Fatal("CHẤP NHẬN payload đã bị sửa")
	}
}

// TestRejectsForeignBundleID: hoá đơn thật, chữ ký thật của Apple — nhưng của
// MỘT APP KHÁC. Kẻ tấn công mua gói rẻ nhất ở app nào đó rồi gửi hoá đơn sang đây.
func TestRejectsForeignBundleID(t *testing.T) {
	c := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	v := newVerifier(t, c)

	p := txPayload()
	p["bundleId"] = "com.app-khac.gi-do"

	_, err := v.VerifyTransaction(c.sign(t, p))
	if !errors.Is(err, ErrWrongBundleID) {
		t.Fatalf("lỗi = %v, muốn ErrWrongBundleID", err)
	}
}

// TestRejectsSandboxOnProduction: giao dịch sandbox miễn phí và ai có tài khoản
// nhà phát triển cũng tạo được. Chấp nhận chúng trên production là phát dung
// lượng miễn phí.
func TestRejectsSandboxOnProduction(t *testing.T) {
	c := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	v := newVerifier(t, c)

	p := txPayload()
	p["environment"] = "Sandbox"

	if _, err := v.VerifyTransaction(c.sign(t, p)); err == nil {
		t.Fatal("CHẤP NHẬN giao dịch Sandbox trên môi trường Production")
	}
}

func TestRevocationDateMarksRevoked(t *testing.T) {
	c := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	v := newVerifier(t, c)

	p := txPayload()
	p["revocationDate"] = time.Now().UnixMilli()

	got, err := v.VerifyTransaction(c.sign(t, p))
	if err != nil {
		t.Fatalf("VerifyTransaction: %v", err)
	}
	if !got.Revoked {
		t.Error("revocationDate có giá trị nhưng Revoked = false — người hoàn tiền vẫn giữ quyền lợi")
	}
}

// TestNotificationVerifiesInnerJWS: payload lồng nhau. JWS ngoài chứa thông báo,
// bên trong lại là một JWS giao dịch ký riêng. Tin JWS trong mà không kiểm là bỏ
// qua đúng lớp bảo vệ vừa dựng.
func TestNotificationVerifiesInnerJWS(t *testing.T) {
	c := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	v := newVerifier(t, c)

	inner := c.sign(t, txPayload())
	note := c.sign(t, map[string]any{
		"notificationType": "DID_RENEW",
		"data": map[string]any{
			"bundleId":              testBundleID,
			"environment":           "Production",
			"signedTransactionInfo": inner,
		},
	})

	p, kind, err := v.VerifyNotification(note)
	if err != nil {
		t.Fatalf("VerifyNotification: %v", err)
	}
	if kind != "DID_RENEW" {
		t.Errorf("notificationType = %q", kind)
	}
	if p.TransactionID != "2000000123456789" {
		t.Errorf("TransactionID = %q", p.TransactionID)
	}
}

// TestNotificationRejectsForgedInnerJWS: thông báo ngoài ký hợp lệ bởi Apple,
// nhưng JWS giao dịch bên trong do kẻ tấn công tự ký.
func TestNotificationRejectsForgedInnerJWS(t *testing.T) {
	c := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	attacker := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	v := newVerifier(t, c)

	forged := txPayload()
	forged["productId"] = "storage_2tb_monthly"
	inner := attacker.sign(t, forged)

	note := c.sign(t, map[string]any{
		"notificationType": "DID_RENEW",
		"data": map[string]any{
			"bundleId":              testBundleID,
			"signedTransactionInfo": inner,
		},
	})

	if _, _, err := v.VerifyNotification(note); err == nil {
		t.Fatal("CHẤP NHẬN thông báo có JWS giao dịch bên trong bị giả mạo")
	}
}

func TestRefundNotificationMarksRevoked(t *testing.T) {
	c := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	v := newVerifier(t, c)

	// Giao dịch chưa có revocationDate — Apple đôi khi gửi vậy. Loại thông báo
	// là tín hiệu bổ sung.
	inner := c.sign(t, txPayload())
	note := c.sign(t, map[string]any{
		"notificationType": "REFUND",
		"data": map[string]any{
			"bundleId":              testBundleID,
			"signedTransactionInfo": inner,
		},
	})

	p, _, err := v.VerifyNotification(note)
	if err != nil {
		t.Fatalf("VerifyNotification: %v", err)
	}
	if !p.Revoked {
		t.Error("thông báo REFUND nhưng Revoked = false")
	}
}

func TestConfigValidation(t *testing.T) {
	c := newChain(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))

	if _, err := New(Config{BundleID: testBundleID}); !errors.Is(err, ErrNoRootConfigured) {
		t.Errorf("thiếu chứng thư gốc: lỗi = %v, muốn ErrNoRootConfigured", err)
	}
	if _, err := New(Config{AppleRootCertsPEM: c.rootPEM}); err == nil {
		t.Error("thiếu bundleID mà không báo lỗi")
	}
	if _, err := New(Config{
		AppleRootCertsPEM: []byte("không phải PEM"), BundleID: testBundleID,
	}); err == nil {
		t.Error("PEM hỏng mà không báo lỗi")
	}
}
