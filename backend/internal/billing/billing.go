// Package billing xử lý mua dung lượng qua In-App Purchase.
//
// Bối cảnh thương mại quan trọng (xem docs/adr/0002-auth-and-storage.md): bán
// dung lượng do mình quản lý là dịch vụ số, nên ở mọi storefront TRỪ Hoa Kỳ thì
// bắt buộc qua IAP và Apple thu 15-30%. Con số đó phải nằm trong mô hình giá.
//
// Nguyên tắc bảo mật bao trùm: KHÔNG BAO GIỜ tin lời client nói rằng đã mua.
// Client sửa được thì khai được là đã mua gói lớn nhất. Mọi giao dịch phải được
// xác minh với Apple hoặc Google trước khi cấp quyền lợi.
package billing

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Platform string

const (
	PlatformApple  Platform = "apple"
	PlatformGoogle Platform = "google"
)

var (
	ErrUnknownProduct      = errors.New("mã sản phẩm không xác định")
	ErrReceiptInvalid      = errors.New("hoá đơn không hợp lệ")
	ErrAlreadyClaimed      = errors.New("giao dịch đã thuộc về tài khoản khác")
	ErrEntitlementNotFound = errors.New("không tìm thấy quyền lợi")
)

const GiB = 1 << 30

// Product là một gói dung lượng bán trên store.
type Product struct {
	ID           string
	Name         string
	StorageBytes int64
}

// Catalog ánh xạ mã sản phẩm của store sang quyền lợi.
//
// Cố ý giữ ở phía SERVER chứ không để client gửi lên dung lượng tương ứng: client
// gửi "gói này 10TB" thì server không có cách nào kiểm chứng. Server phải là nơi
// duy nhất biết mỗi mã sản phẩm đáng bao nhiêu.
type Catalog map[string]Product

func DefaultCatalog() Catalog {
	return Catalog{
		"storage_100gb_monthly": {ID: "storage_100gb_monthly", Name: "100 GB", StorageBytes: 100 * GiB},
		"storage_1tb_monthly":   {ID: "storage_1tb_monthly", Name: "1 TB", StorageBytes: 1024 * GiB},
		"storage_2tb_monthly":   {ID: "storage_2tb_monthly", Name: "2 TB", StorageBytes: 2048 * GiB},
	}
}

// FreeQuotaBytes là hạn mức khi chưa mua gì.
//
// Đủ để thử sản phẩm với vài chục ảnh preview, không đủ để lưu một buổi chụp.
// Đó là chủ ý: giá trị chính của app không nằm ở việc ta giữ file hộ, mà ở màu
// và AI — người dùng có thể dùng ProviderDevice hoặc Drive của họ mà không tốn
// đồng nào.
const FreeQuotaBytes = 2 * GiB

// Purchase là giao dịch ĐÃ ĐƯỢC XÁC MINH với store.
type Purchase struct {
	Platform Platform
	// TransactionID phải ỔN ĐỊNH qua các lần gia hạn. Với Apple là
	// originalTransactionId; với Google là purchaseToken. Dùng id của từng lần
	// gia hạn sẽ khiến mỗi tháng tạo ra một quyền lợi mới thay vì cập nhật cái cũ.
	TransactionID string
	ProductID     string
	ExpiresAt     time.Time
	// Revoked = true khi giao dịch bị hoàn tiền hoặc huỷ. Store báo qua server
	// notification; bỏ qua tín hiệu này nghĩa là người hoàn tiền vẫn giữ quyền lợi.
	Revoked bool
}

// ReceiptVerifier gọi tới Apple hoặc Google để xác minh hoá đơn.
//
// Là interface để test được toàn bộ logic quyền lợi mà không cần gọi mạng thật,
// và để bản triển khai thật (App Store Server API, Google Play Developer API) có
// thể thay vào sau mà không đụng tới logic.
type ReceiptVerifier interface {
	Verify(ctx context.Context, platform Platform, receipt string) (Purchase, error)
}

type Entitlement struct {
	UserID        string
	Platform      Platform
	TransactionID string
	ProductID     string
	StorageBytes  int64
	ExpiresAt     time.Time
	Revoked       bool
	UpdatedAt     time.Time
}

func (e Entitlement) ActiveAt(t time.Time) bool {
	return !e.Revoked && t.Before(e.ExpiresAt)
}

type Repo interface {
	// ByTransaction tra theo (platform, transactionID). Đây là khoá chống trùng
	// và chống một giao dịch được dùng cho nhiều tài khoản.
	ByTransaction(ctx context.Context, p Platform, txID string) (Entitlement, error)
	Upsert(ctx context.Context, e Entitlement) error
	OfUser(ctx context.Context, userID string) ([]Entitlement, error)
}

type Service struct {
	repo     Repo
	verifier ReceiptVerifier
	catalog  Catalog
	now      func() time.Time
}

func NewService(repo Repo, v ReceiptVerifier, catalog Catalog, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	if catalog == nil {
		catalog = DefaultCatalog()
	}
	return &Service{repo: repo, verifier: v, catalog: catalog, now: now}
}

// Redeem xác minh hoá đơn rồi cấp quyền lợi.
//
// Ba lớp phòng thủ, mỗi lớp chặn một cách lạm dụng khác nhau:
//
//  1. Xác minh với store — chặn hoá đơn tự chế.
//  2. Tra theo TransactionID trước khi ghi — chặn phát lại cùng một hoá đơn để
//     cộng dồn dung lượng nhiều lần.
//  3. Từ chối nếu giao dịch đã thuộc tài khoản khác — chặn một lần mua được chia
//     cho cả một nhóm bạn bè.
func (s *Service) Redeem(ctx context.Context, userID string, platform Platform, receipt string) (Entitlement, error) {
	if userID == "" {
		return Entitlement{}, fmt.Errorf("thiếu userID")
	}

	// Lớp 1.
	p, err := s.verifier.Verify(ctx, platform, receipt)
	if err != nil {
		return Entitlement{}, fmt.Errorf("%w: %v", ErrReceiptInvalid, err)
	}
	if p.TransactionID == "" {
		return Entitlement{}, fmt.Errorf("%w: thiếu mã giao dịch", ErrReceiptInvalid)
	}

	product, ok := s.catalog[p.ProductID]
	if !ok {
		// Mã sản phẩm lạ có thể là hoá đơn của app khác, hoặc cấu hình store lệch
		// với server. Cả hai đều phải dừng lại chứ không đoán.
		return Entitlement{}, fmt.Errorf("%w: %q", ErrUnknownProduct, p.ProductID)
	}

	// Lớp 2 và 3.
	existing, err := s.repo.ByTransaction(ctx, p.Platform, p.TransactionID)
	switch {
	case err == nil:
		if existing.UserID != userID {
			return Entitlement{}, ErrAlreadyClaimed
		}
	case !errors.Is(err, ErrEntitlementNotFound):
		return Entitlement{}, err
	}

	e := Entitlement{
		UserID:        userID,
		Platform:      p.Platform,
		TransactionID: p.TransactionID,
		ProductID:     p.ProductID,
		StorageBytes:  product.StorageBytes,
		ExpiresAt:     p.ExpiresAt,
		Revoked:       p.Revoked,
		UpdatedAt:     s.now(),
	}
	if err := s.repo.Upsert(ctx, e); err != nil {
		return Entitlement{}, err
	}
	return e, nil
}

// QuotaBytes tính hạn mức hiện tại của người dùng.
//
// Cộng dồn các quyền lợi còn hiệu lực thay vì lấy gói lớn nhất: người dùng mua
// thêm gói khi thiếu chỗ, và họ mong đợi được cộng vào. Lấy max sẽ khiến lần mua
// thứ hai trông như không có tác dụng gì.
func (s *Service) QuotaBytes(ctx context.Context, userID string) (int64, error) {
	list, err := s.repo.OfUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	total := int64(FreeQuotaBytes)
	now := s.now()
	for _, e := range list {
		if e.ActiveAt(now) {
			total += e.StorageBytes
		}
	}
	return total, nil
}

// HandleStoreNotification xử lý thông báo server-to-server của store.
//
// Bắt buộc phải có: gia hạn, hết hạn, hoàn tiền và huỷ đều xảy ra NGOÀI app.
// Nếu chỉ cập nhật quyền lợi lúc người dùng mở app, một người hoàn tiền rồi
// không mở app nữa sẽ giữ dung lượng vĩnh viễn.
func (s *Service) HandleStoreNotification(ctx context.Context, p Purchase) error {
	existing, err := s.repo.ByTransaction(ctx, p.Platform, p.TransactionID)
	if err != nil {
		// Thông báo cho giao dịch chưa từng thấy: bỏ qua thay vì tạo mới. Không
		// có userID thì quyền lợi thuộc về ai? Người dùng sẽ tự đồng bộ khi mở app.
		if errors.Is(err, ErrEntitlementNotFound) {
			return nil
		}
		return err
	}

	existing.ExpiresAt = p.ExpiresAt
	existing.Revoked = p.Revoked
	existing.UpdatedAt = s.now()
	if product, ok := s.catalog[p.ProductID]; ok {
		existing.StorageBytes = product.StorageBytes
	}
	return s.repo.Upsert(ctx, existing)
}
