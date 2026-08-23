// Package storage trừu tượng hoá nơi lưu ảnh, để người dùng tự chọn.
//
// Xem docs/adr/0002-auth-and-storage.md. Tóm tắt lý do: một buổi chụp là hơn
// 100GB RAW. Mặc định nhét toàn bộ vào hạ tầng của mình vừa đắt vừa không cần
// thiết — và với storefront ngoài Hoa Kỳ, bán dung lượng của mình còn mất 15-30%
// hoa hồng cho Apple. Cho người dùng dùng Drive của chính họ vừa rẻ hơn cho ta,
// vừa không phát sinh giao dịch số nào để Apple thu phí.
package storage

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type ProviderID string

const (
	// ProviderDevice: ảnh ở lại trên thẻ nhớ và điện thoại. Không ai trả tiền
	// dung lượng. Đây là mặc định, khớp với kiến trúc "để ảnh trên thẻ" của ADR 0001.
	ProviderDevice ProviderID = "device"
	// ProviderManaged: hạ tầng của ta. Người dùng mua dung lượng.
	ProviderManaged ProviderID = "managed"
	// ProviderGoogleDrive: Drive của người dùng, scope drive.file.
	ProviderGoogleDrive ProviderID = "google_drive"
	// ProviderICloud: iCloud của người dùng.
	ProviderICloud ProviderID = "icloud"
)

// Capability mô tả nhà cung cấp làm được gì.
//
// Cùng khuôn mẫu với CameraCapability bên mobile, và cùng lý do: rẽ nhánh theo
// KHẢ NĂNG chứ không theo tên nhà cung cấp. Viết `if provider == ProviderICloud`
// rải rác trong code là cách chắc chắn để bỏ sót một nhánh khi thêm nhà cung cấp
// thứ năm.
type Capability string

const (
	// CapServerSideRender: server đọc được bytes, nên render RAW chất lượng cao
	// phía server khả dụng.
	//
	// KHÔNG phải nhà cung cấp nào cũng có. Với icloud và device, server không
	// bao giờ thấy file — bản xuất chất lượng cao phải render trên thiết bị hoặc
	// không có. Đây là khác biệt TÍNH NĂNG thật, phải hiển thị cho người dùng
	// thấy lúc họ chọn, không phải để họ phát hiện khi bấm xuất file.
	CapServerSideRender Capability = "serverSideRender"

	// CapEnforcedQuota: ta cưỡng chế được hạn mức. Chỉ đúng với managed —
	// với Drive và iCloud, hạn mức là chuyện giữa người dùng và Google/Apple.
	CapEnforcedQuota Capability = "enforcedQuota"

	// CapDurable: dữ liệu không biến mất khi người dùng thu hồi quyền hoặc hết
	// dung lượng ở dịch vụ bên thứ ba.
	CapDurable Capability = "durable"
)

var (
	ErrQuotaExceeded    = errors.New("vượt quá dung lượng")
	ErrNotLinked        = errors.New("chưa liên kết tài khoản lưu trữ")
	ErrUnsupported      = errors.New("nhà cung cấp không hỗ trợ thao tác này")
	ErrProviderNotFound = errors.New("không tìm thấy nhà cung cấp")
)

// Target là chỉ dẫn để client tự upload.
//
// File KHÔNG đi qua Go API. Client upload thẳng lên nơi lưu trữ. Cho một NEF
// 60MB chảy qua handler sẽ giữ goroutine và băng thông của API server suốt thời
// gian upload; với hàng chục client đang tether cùng lúc thì đó là cách làm sập
// service.
type Target struct {
	Provider  ProviderID        `json:"provider"`
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers,omitempty"`
	Key       string            `json:"key"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

// Usage là tình trạng dung lượng.
type Usage struct {
	Provider  ProviderID `json:"provider"`
	UsedBytes int64      `json:"usedBytes"`
	// LimitBytes = 0 nghĩa là KHÔNG BIẾT hoặc không áp dụng, không phải "bằng 0".
	// Với Drive và iCloud, ta chỉ đọc được nếu nhà cung cấp cho biết.
	LimitBytes int64 `json:"limitBytes"`
	Enforced   bool  `json:"enforced"`
}

func (u Usage) Remaining() int64 {
	if !u.Enforced || u.LimitBytes <= 0 {
		return -1 // không xác định
	}
	if r := u.LimitBytes - u.UsedBytes; r > 0 {
		return r
	}
	return 0
}

type Provider interface {
	ID() ProviderID
	Capabilities() []Capability
	// Upload trả chỉ dẫn để client tự tải lên.
	Upload(ctx context.Context, userID, key string, size int64) (Target, error)
	// Download trả URL đọc có thời hạn.
	Download(ctx context.Context, userID, key string) (string, error)
	Delete(ctx context.Context, userID, key string) error
	Usage(ctx context.Context, userID string) (Usage, error)
}

func Has(p Provider, c Capability) bool {
	for _, got := range p.Capabilities() {
		if got == c {
			return true
		}
	}
	return false
}

// Registry chứa các nhà cung cấp đã cấu hình.
type Registry struct {
	providers map[ProviderID]Provider
}

func NewRegistry(ps ...Provider) *Registry {
	m := make(map[ProviderID]Provider, len(ps))
	for _, p := range ps {
		m[p.ID()] = p
	}
	return &Registry{providers: m}
}

func (r *Registry) Get(id ProviderID) (Provider, error) {
	p, ok := r.providers[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrProviderNotFound, id)
	}
	return p, nil
}

// Options mô tả các lựa chọn để hiển thị cho người dùng.
//
// Trả kèm capabilities chứ không chỉ tên: người dùng phải thấy được rằng chọn
// iCloud nghĩa là mất khả năng render phía server, TRƯỚC khi chọn.
type Option struct {
	Provider     ProviderID   `json:"provider"`
	Capabilities []Capability `json:"capabilities"`
	// Warning là cảnh báo phải hiển thị ngay tại màn hình chọn, không phải giấu
	// trong điều khoản sử dụng.
	Warning string `json:"warning,omitempty"`
}

func (r *Registry) Options() []Option {
	out := make([]Option, 0, len(r.providers))
	for id, p := range r.providers {
		out = append(out, Option{
			Provider:     id,
			Capabilities: p.Capabilities(),
			Warning:      warningFor(id),
		})
	}
	return out
}

// warningFor trả cảnh báo cho các lựa chọn có rủi ro mất dữ liệu.
//
// Đây là yêu cầu sản phẩm, không phải trang trí: với Drive và iCloud, dữ liệu
// nằm ngoài tầm kiểm soát của app. Người dùng hết dung lượng, thu hồi quyền,
// hoặc xoá file trực tiếp thì ảnh biến mất và ta không làm gì được. Nói rõ lúc
// họ chọn là khác biệt giữa một sản phẩm trung thực và một vụ mất ảnh cưới.
func warningFor(id ProviderID) string {
	switch id {
	case ProviderGoogleDrive:
		return "Ảnh lưu trong Google Drive của bạn. Nếu bạn hết dung lượng Drive, " +
			"thu hồi quyền truy cập, hoặc xoá file trực tiếp trong Drive, ảnh sẽ " +
			"biến mất khỏi ứng dụng và không khôi phục được."
	case ProviderICloud:
		return "Ảnh lưu trong iCloud của bạn. Ứng dụng không đọc được file để kết " +
			"xuất chất lượng cao phía máy chủ, và ảnh sẽ mất nếu bạn hết dung lượng " +
			"iCloud hoặc xoá file trực tiếp."
	case ProviderDevice:
		return "Ảnh chỉ nằm trên thẻ nhớ và điện thoại. Mất máy hoặc mất thẻ là mất ảnh."
	default:
		return ""
	}
}
