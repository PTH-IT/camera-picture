package billing

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Bộ test này tập trung vào chống lạm dụng, không phải vào việc CRUD chạy đúng.
// Mua bán là nơi người dùng có động cơ tài chính rõ ràng để lách, nên mỗi quy tắc
// đều được kiểm bằng một kịch bản lạm dụng cụ thể.

// fakeVerifier đóng vai Apple/Google. Hoá đơn hợp lệ là chuỗi đã đăng ký trước;
// mọi thứ khác bị từ chối — giống cách store thật hành xử với hoá đơn tự chế.
type fakeVerifier struct {
	valid map[string]Purchase
}

func (f *fakeVerifier) Verify(_ context.Context, _ Platform, receipt string) (Purchase, error) {
	p, ok := f.valid[receipt]
	if !ok {
		return Purchase{}, fmt.Errorf("store từ chối hoá đơn")
	}
	return p, nil
}

func newSvc(t *testing.T, now time.Time) (*Service, *fakeVerifier, *MemRepo) {
	t.Helper()
	v := &fakeVerifier{valid: map[string]Purchase{}}
	repo := NewMemRepo()
	return NewService(repo, v, DefaultCatalog(), func() time.Time { return now }), v, repo
}

// TestForgedReceiptRejected: client tự chế hoá đơn. Không xác minh với store thì
// bất kỳ ai cũng tự cấp cho mình gói lớn nhất.
func TestForgedReceiptRejected(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newSvc(t, time.Now())

	_, err := svc.Redeem(ctx, "u1", PlatformApple, "hoa-don-tu-che")
	if !errors.Is(err, ErrReceiptInvalid) {
		t.Fatalf("lỗi = %v, muốn ErrReceiptInvalid", err)
	}

	q, err := svc.QuotaBytes(ctx, "u1")
	if err != nil {
		t.Fatalf("QuotaBytes: %v", err)
	}
	if q != FreeQuotaBytes {
		t.Errorf("hạn mức = %d, muốn %d — hoá đơn giả đã cấp quyền lợi", q, int64(FreeQuotaBytes))
	}
}

// TestReplayDoesNotStack: gửi lại đúng một hoá đơn nhiều lần. Không chống là mua
// một lần rồi bấm nút 50 lần để có 50 TB.
func TestReplayDoesNotStack(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	svc, v, _ := newSvc(t, now)

	v.valid["r1"] = Purchase{
		Platform: PlatformApple, TransactionID: "tx-1",
		ProductID: "storage_1tb_monthly", ExpiresAt: now.Add(30 * 24 * time.Hour),
	}

	for i := 0; i < 5; i++ {
		if _, err := svc.Redeem(ctx, "u1", PlatformApple, "r1"); err != nil {
			t.Fatalf("lần %d: %v", i, err)
		}
	}

	q, _ := svc.QuotaBytes(ctx, "u1")
	want := int64(FreeQuotaBytes) + 1024*GiB
	if q != want {
		t.Errorf("hạn mức = %d GiB, muốn %d GiB — phát lại đã cộng dồn", q/GiB, want/GiB)
	}
}

// TestTransactionBoundToOneAccount: một người mua rồi chia hoá đơn cho cả nhóm.
func TestTransactionBoundToOneAccount(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	svc, v, _ := newSvc(t, now)

	v.valid["r1"] = Purchase{
		Platform: PlatformApple, TransactionID: "tx-1",
		ProductID: "storage_1tb_monthly", ExpiresAt: now.Add(30 * 24 * time.Hour),
	}

	if _, err := svc.Redeem(ctx, "nguoi-mua", PlatformApple, "r1"); err != nil {
		t.Fatalf("người mua: %v", err)
	}

	_, err := svc.Redeem(ctx, "nguoi-khac", PlatformApple, "r1")
	if !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("lỗi = %v, muốn ErrAlreadyClaimed — một lần mua dùng cho nhiều tài khoản", err)
	}

	q, _ := svc.QuotaBytes(ctx, "nguoi-khac")
	if q != FreeQuotaBytes {
		t.Errorf("tài khoản thứ hai có hạn mức %d GiB, muốn %d GiB", q/GiB, int64(FreeQuotaBytes)/GiB)
	}
}

func TestUnknownProductRejected(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	svc, v, _ := newSvc(t, now)

	v.valid["r1"] = Purchase{
		Platform: PlatformApple, TransactionID: "tx-1",
		ProductID: "storage_999tb_free", ExpiresAt: now.Add(time.Hour),
	}
	if _, err := svc.Redeem(ctx, "u1", PlatformApple, "r1"); !errors.Is(err, ErrUnknownProduct) {
		t.Errorf("lỗi = %v, muốn ErrUnknownProduct", err)
	}
}

// TestExpiredEntitlementDropsQuota: hết hạn thuê bao thì mất dung lượng.
func TestExpiredEntitlementDropsQuota(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	repo := NewMemRepo()
	v := &fakeVerifier{valid: map[string]Purchase{"r1": {
		Platform: PlatformApple, TransactionID: "tx-1",
		ProductID: "storage_100gb_monthly", ExpiresAt: clock.Add(24 * time.Hour),
	}}}
	svc := NewService(repo, v, DefaultCatalog(), func() time.Time { return clock })

	if _, err := svc.Redeem(ctx, "u1", PlatformApple, "r1"); err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if q, _ := svc.QuotaBytes(ctx, "u1"); q != int64(FreeQuotaBytes)+100*GiB {
		t.Fatalf("hạn mức ngay sau khi mua = %d GiB", q/GiB)
	}

	clock = clock.Add(48 * time.Hour)
	if q, _ := svc.QuotaBytes(ctx, "u1"); q != FreeQuotaBytes {
		t.Errorf("sau khi hết hạn hạn mức = %d GiB, muốn %d GiB", q/GiB, int64(FreeQuotaBytes)/GiB)
	}
}

// TestRefundRevokesEntitlement: hoàn tiền xảy ra NGOÀI app. Chỉ cập nhật lúc mở
// app thì người hoàn tiền rồi không mở app nữa sẽ giữ dung lượng vĩnh viễn.
func TestRefundRevokesEntitlement(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	svc, v, _ := newSvc(t, now)

	v.valid["r1"] = Purchase{
		Platform: PlatformApple, TransactionID: "tx-1",
		ProductID: "storage_1tb_monthly", ExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	if _, err := svc.Redeem(ctx, "u1", PlatformApple, "r1"); err != nil {
		t.Fatalf("Redeem: %v", err)
	}

	err := svc.HandleStoreNotification(ctx, Purchase{
		Platform: PlatformApple, TransactionID: "tx-1",
		ProductID: "storage_1tb_monthly", ExpiresAt: now.Add(30 * 24 * time.Hour),
		Revoked: true,
	})
	if err != nil {
		t.Fatalf("HandleStoreNotification: %v", err)
	}

	if q, _ := svc.QuotaBytes(ctx, "u1"); q != FreeQuotaBytes {
		t.Errorf("sau hoàn tiền hạn mức = %d GiB, muốn %d GiB", q/GiB, int64(FreeQuotaBytes)/GiB)
	}
}

// TestMultiplePurchasesAdd: người dùng mua thêm khi thiếu chỗ và mong đợi được
// cộng vào. Lấy max sẽ khiến lần mua thứ hai trông như vô tác dụng.
func TestMultiplePurchasesAdd(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	svc, v, _ := newSvc(t, now)

	exp := now.Add(30 * 24 * time.Hour)
	v.valid["r1"] = Purchase{Platform: PlatformApple, TransactionID: "tx-1", ProductID: "storage_100gb_monthly", ExpiresAt: exp}
	v.valid["r2"] = Purchase{Platform: PlatformGoogle, TransactionID: "tx-2", ProductID: "storage_1tb_monthly", ExpiresAt: exp}

	cases := []struct {
		p Platform
		r string
	}{{PlatformApple, "r1"}, {PlatformGoogle, "r2"}}
	for _, c := range cases {
		if _, err := svc.Redeem(ctx, "u1", c.p, c.r); err != nil {
			t.Fatalf("Redeem %s: %v", c.r, err)
		}
	}

	q, _ := svc.QuotaBytes(ctx, "u1")
	want := int64(FreeQuotaBytes) + 100*GiB + 1024*GiB
	if q != want {
		t.Errorf("hạn mức = %d GiB, muốn %d GiB", q/GiB, want/GiB)
	}
}

// TestNotificationForUnknownTransactionIsIgnored: không có userID thì quyền lợi
// thuộc về ai? Tạo mới là sai; bỏ qua và để người dùng tự đồng bộ khi mở app.
func TestNotificationForUnknownTransactionIsIgnored(t *testing.T) {
	ctx := context.Background()
	svc, _, repo := newSvc(t, time.Now())

	err := svc.HandleStoreNotification(ctx, Purchase{
		Platform: PlatformApple, TransactionID: "chua-tung-thay",
		ProductID: "storage_1tb_monthly", ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("phải bỏ qua chứ không lỗi: %v", err)
	}
	if _, err := repo.ByTransaction(ctx, PlatformApple, "chua-tung-thay"); !errors.Is(err, ErrEntitlementNotFound) {
		t.Error("đã tạo quyền lợi mồ côi không gắn với tài khoản nào")
	}
}

func TestFreeQuotaWithoutPurchase(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newSvc(t, time.Now())

	q, err := svc.QuotaBytes(ctx, "nguoi-moi")
	if err != nil {
		t.Fatalf("QuotaBytes: %v", err)
	}
	if q != FreeQuotaBytes {
		t.Errorf("hạn mức mặc định = %d, muốn %d", q, int64(FreeQuotaBytes))
	}
}
