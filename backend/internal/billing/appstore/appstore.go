// Package appstore xác minh giao dịch đã ký của Apple (App Store Server API v2
// và App Store Server Notifications v2).
//
// Điểm mấu chốt để hiểu package này: Apple gửi giao dịch dưới dạng JWS, và chuỗi
// chứng thư dùng để ký nằm NGAY TRONG header `x5c` của chính JWS đó. Nghĩa là kẻ
// tấn công tự ký được một JWS "hợp lệ" bằng chứng thư của chính hắn và đính kèm
// chuỗi của hắn.
//
// Thứ duy nhất phân biệt được thật với giả là: chuỗi đó có bắt nguồn từ CHỨNG THƯ
// GỐC CỦA APPLE mà ta tự cấu hình hay không. Bỏ bước kiểm tra gốc — chỉ kiểm chữ
// ký khớp với x5c — là chấp nhận mọi hoá đơn tự chế, và lỗi đó không có triệu
// chứng nào ngoài việc ai cũng có dung lượng không giới hạn.
package appstore

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/hauph/camera/backend/internal/billing"
)

var (
	ErrNoCertChain      = errors.New("JWS không có chuỗi chứng thư x5c")
	ErrUntrustedChain   = errors.New("chuỗi chứng thư không bắt nguồn từ chứng thư gốc của Apple")
	ErrInvalidSignature = errors.New("chữ ký không hợp lệ")
	ErrWrongBundleID    = errors.New("giao dịch thuộc ứng dụng khác")
	ErrNoRootConfigured = errors.New("chưa cấu hình chứng thư gốc của Apple")
)

// Verifier kiểm tra JWS do Apple ký.
type Verifier struct {
	roots *x509.CertPool
	// bundleID của app. Không kiểm là chấp nhận hoá đơn hợp lệ của MỘT APP KHÁC —
	// kẻ tấn công mua gói rẻ nhất ở app nào đó rồi gửi hoá đơn đó sang đây.
	bundleID string
	// environment: "Production" hoặc "Sandbox". Không kiểm là chấp nhận giao dịch
	// sandbox trên production, tức là dung lượng miễn phí cho bất kỳ ai có tài
	// khoản nhà phát triển.
	requireEnvironment string
	now                func() time.Time
}

type Config struct {
	// AppleRootCertsPEM là các chứng thư gốc của Apple ở dạng PEM.
	//
	// Tải từ apple.com/certificateauthority (Apple Root CA - G3). Nhúng vào cấu
	// hình chứ KHÔNG tải lúc chạy: tải qua mạng nghĩa là ai kiểm soát được đường
	// mạng đó sẽ thay được gốc tin cậy, và khi ấy toàn bộ lớp xác minh vô nghĩa.
	AppleRootCertsPEM []byte
	BundleID          string
	// Environment rỗng nghĩa là chấp nhận cả hai — CHỈ dùng khi phát triển.
	Environment string
	Now         func() time.Time
}

func New(cfg Config) (*Verifier, error) {
	if len(cfg.AppleRootCertsPEM) == 0 {
		return nil, ErrNoRootConfigured
	}
	if cfg.BundleID == "" {
		return nil, errors.New("thiếu bundleID")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cfg.AppleRootCertsPEM) {
		return nil, errors.New("không đọc được chứng thư gốc từ PEM")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Verifier{
		roots:              roots,
		bundleID:           cfg.BundleID,
		requireEnvironment: cfg.Environment,
		now:                now,
	}, nil
}

// transactionPayload là các trường của JWSTransactionDecodedPayload mà ta dùng.
type transactionPayload struct {
	BundleID  string `json:"bundleId"`
	ProductID string `json:"productId"`
	// OriginalTransactionId là khoá ỔN ĐỊNH qua các lần gia hạn.
	//
	// Dùng transactionId (đổi mỗi lần gia hạn) sẽ khiến mỗi tháng tạo ra một
	// quyền lợi MỚI thay vì cập nhật cái cũ, và dung lượng của người dùng tăng
	// vô hạn theo thời gian. Đây là lỗi rất dễ mắc vì cả hai trường đều tồn tại
	// và đều trông hợp lý.
	OriginalTransactionID string `json:"originalTransactionId"`
	TransactionID         string `json:"transactionId"`
	// Thời gian tính bằng MILI giây, không phải giây.
	ExpiresDateMS  int64  `json:"expiresDate"`
	PurchaseDateMS int64  `json:"purchaseDate"`
	RevocationDate int64  `json:"revocationDate"`
	Type           string `json:"type"`
	Environment    string `json:"environment"`
}

// VerifyTransaction kiểm tra một JWS giao dịch và trả về Purchase đã xác minh.
func (v *Verifier) VerifyTransaction(signedJWS string) (billing.Purchase, error) {
	var claims transactionPayload
	if err := v.parseAndVerify(signedJWS, &claims); err != nil {
		return billing.Purchase{}, err
	}

	if claims.BundleID != v.bundleID {
		return billing.Purchase{}, fmt.Errorf("%w: %q", ErrWrongBundleID, claims.BundleID)
	}
	if v.requireEnvironment != "" && claims.Environment != v.requireEnvironment {
		return billing.Purchase{}, fmt.Errorf(
			"môi trường %q không khớp %q — giao dịch sandbox không được tính trên production",
			claims.Environment, v.requireEnvironment)
	}
	if claims.OriginalTransactionID == "" {
		return billing.Purchase{}, errors.New("thiếu originalTransactionId")
	}

	p := billing.Purchase{
		Platform:      billing.PlatformApple,
		TransactionID: claims.OriginalTransactionID,
		ProductID:     claims.ProductID,
		// revocationDate khác 0 nghĩa là đã hoàn tiền hoặc bị thu hồi. Bỏ qua
		// trường này là để người hoàn tiền giữ nguyên quyền lợi.
		Revoked: claims.RevocationDate != 0,
	}
	if claims.ExpiresDateMS > 0 {
		p.ExpiresAt = time.UnixMilli(claims.ExpiresDateMS).UTC()
	}
	return p, nil
}

// notificationPayload là responseBodyV2DecodedPayload.
type notificationPayload struct {
	NotificationType string `json:"notificationType"`
	Subtype          string `json:"subtype"`
	Data             struct {
		BundleID              string `json:"bundleId"`
		Environment           string `json:"environment"`
		SignedTransactionInfo string `json:"signedTransactionInfo"`
	} `json:"data"`
}

// VerifyNotification kiểm tra thông báo server-to-server của App Store.
//
// Bắt buộc phải xử lý: gia hạn, hết hạn, hoàn tiền và huỷ đều xảy ra NGOÀI app.
// Chỉ cập nhật quyền lợi lúc người dùng mở app thì một người hoàn tiền rồi không
// mở app nữa sẽ giữ dung lượng vĩnh viễn.
//
// Payload lồng nhau: JWS ngoài chứa thông báo, bên trong lại là một JWS giao dịch
// đã ký riêng. CẢ HAI đều phải được xác minh — tin JWS trong mà không kiểm là bỏ
// qua đúng lớp bảo vệ mình vừa dựng.
func (v *Verifier) VerifyNotification(signedPayload string) (billing.Purchase, string, error) {
	var note notificationPayload
	if err := v.parseAndVerify(signedPayload, &note); err != nil {
		return billing.Purchase{}, "", err
	}
	if note.Data.SignedTransactionInfo == "" {
		return billing.Purchase{}, note.NotificationType, nil
	}

	p, err := v.VerifyTransaction(note.Data.SignedTransactionInfo)
	if err != nil {
		return billing.Purchase{}, note.NotificationType, err
	}

	// REFUND và REVOKE đôi khi tới mà revocationDate chưa có trong payload giao
	// dịch. Loại thông báo là tín hiệu bổ sung, không thay thế.
	switch note.NotificationType {
	case "REFUND", "REVOKE":
		p.Revoked = true
	}
	return p, note.NotificationType, nil
}

// parseAndVerify kiểm chữ ký JWS và chuỗi chứng thư, rồi giải mã payload.
func (v *Verifier) parseAndVerify(signedJWS string, out any) error {
	parser := jwt.NewParser(
		// Apple ký bằng ES256. Chốt cứng để chặn alg confusion — nếu chấp nhận
		// danh sách mở, kẻ tấn công gửi token HS256 ký bằng khoá công khai lấy
		// từ chính x5c và thư viện sẽ coi là hợp lệ.
		jwt.WithValidMethods([]string{"ES256"}),
		// Payload của Apple KHÔNG có exp. Không bật kiểm hết hạn.
		jwt.WithoutClaimsValidation(),
	)

	var raw jwt.MapClaims
	_, err := parser.ParseWithClaims(signedJWS, &raw, func(t *jwt.Token) (any, error) {
		return v.leafKey(t)
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}

	// Đi qua JSON để dùng lại thẻ struct thay vì tự ép kiểu từng trường trong
	// MapClaims — vừa ngắn hơn vừa ít chỗ sai hơn.
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// leafKey rút khoá công khai từ x5c SAU KHI đã kiểm chuỗi tin cậy.
//
// Thứ tự ở đây là điều quan trọng nhất của cả package: kiểm chuỗi TRƯỚC, dùng
// khoá SAU. Đảo lại — lấy khoá từ x5c rồi mới kiểm chuỗi — vẫn "chạy đúng" trong
// mọi test đường thành công, nhưng chấp nhận chữ ký của bất kỳ ai.
func (v *Verifier) leafKey(t *jwt.Token) (any, error) {
	rawChain, ok := t.Header["x5c"].([]any)
	if !ok || len(rawChain) == 0 {
		return nil, ErrNoCertChain
	}

	certs := make([]*x509.Certificate, 0, len(rawChain))
	for i, item := range rawChain {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%w: phần tử x5c thứ %d không phải chuỗi", ErrNoCertChain, i)
		}
		der, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("%w: giải mã x5c thứ %d: %v", ErrNoCertChain, i, err)
		}
		c, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("%w: đọc chứng thư thứ %d: %v", ErrNoCertChain, i, err)
		}
		certs = append(certs, c)
	}

	// x5c theo thứ tự: leaf, trung gian..., gốc.
	leaf := certs[0]
	intermediates := x509.NewCertPool()
	for _, c := range certs[1:] {
		intermediates.AddCert(c)
	}

	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         v.roots,
		Intermediates: intermediates,
		CurrentTime:   v.now(),
		// Apple dùng chứng thư này để ký payload, không phải cho TLS. Đặt
		// KeyUsage rỗng để x509 không đòi ExtKeyUsageServerAuth.
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUntrustedChain, err)
	}

	key, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: khoá công khai không phải ECDSA", ErrInvalidSignature)
	}
	return key, nil
}

// ReceiptVerifier nối Verifier vào billing.ReceiptVerifier.
//
// "Hoá đơn" mà client gửi lên chính là JWS giao dịch đã ký, lấy từ
// StoreKit 2 (Transaction.jwsRepresentation). Không dùng receipt nhị phân kiểu
// StoreKit 1 nữa — Apple đã ngừng endpoint verifyReceipt cho tích hợp mới.
type ReceiptVerifier struct {
	apple *Verifier
}

func NewReceiptVerifier(v *Verifier) *ReceiptVerifier {
	return &ReceiptVerifier{apple: v}
}

func (r *ReceiptVerifier) Verify(_ context.Context, platform billing.Platform, receipt string) (billing.Purchase, error) {
	if platform != billing.PlatformApple {
		return billing.Purchase{}, fmt.Errorf("nền tảng %q không được hỗ trợ ở đây", platform)
	}
	return r.apple.VerifyTransaction(receipt)
}

var _ billing.ReceiptVerifier = (*ReceiptVerifier)(nil)
