// Package protocol định nghĩa hợp đồng dữ liệu giữa app mobile và backend.
//
// Đây là bản phản chiếu của mobile/src/capture/types.ts. Hai file phải khớp nhau
// về tên trường JSON. Không có codegen — nên khi sửa một bên, sửa cả bên kia,
// và bài test đối chiếu trong protocol_test.go tồn tại để bắt lỗi lệch.
//
// Lưu ý về phạm vi: backend KHÔNG làm capture. Capture chạy trên điện thoại
// (iOS + CascableCore). Những kiểu ở đây mô tả thứ điện thoại GỬI LÊN, không
// phải thứ backend tự thu thập.
package protocol

import "time"

// Capability là khả năng của một đường capture cụ thể với một body cụ thể.
// Backend lưu lại để biết ảnh này đến từ nguồn nào và tin được tới đâu —
// ví dụ ảnh từ Android/libgphoto2 sau này sẽ không có live view.
type Capability string

const (
	CapRemoteShutter  Capability = "remoteShutter"
	CapLiveView       Capability = "liveView"
	CapSettingsRead   Capability = "settingsRead"
	CapSettingsWrite  Capability = "settingsWrite"
	CapTetheredEvents Capability = "tetheredEvents"
	CapStorageBrowse  Capability = "storageBrowse"
	// CapPreviewWithoutFullDownload là giả định kiến trúc quan trọng nhất —
	// xem ADR 0001. Nếu false, chiến lược "để ảnh trên thẻ" không dùng được.
	CapPreviewWithoutFullDownload Capability = "previewWithoutFullDownload"
	CapVideoRecord                Capability = "videoRecord"
)

type Transport string

const (
	TransportUSB  Transport = "usb"
	TransportWiFi Transport = "wifi"
)

type CameraInfo struct {
	ID              string       `json:"id"`
	Manufacturer    string       `json:"manufacturer"`
	Model           string       `json:"model"`
	FirmwareVersion string       `json:"firmwareVersion,omitempty"`
	Transport       Transport    `json:"transport"`
	Capabilities    []Capability `json:"capabilities"`
}

func (c CameraInfo) Can(cap Capability) bool {
	for _, have := range c.Capabilities {
		if have == cap {
			return true
		}
	}
	return false
}

type ImageFormat string

const (
	FormatNEF     ImageFormat = "NEF"
	FormatNRW     ImageFormat = "NRW"
	FormatJPEG    ImageFormat = "JPEG"
	FormatHEIF    ImageFormat = "HEIF"
	FormatTIFF    ImageFormat = "TIFF"
	FormatUnknown ImageFormat = "unknown"
)

// ImageRef là metadata một tấm ảnh, không kèm pixel.
//
// Phần lớn ảnh trong một buổi chụp sẽ CHỈ tồn tại ở dạng này phía backend:
// chúng nằm trên thẻ nhớ, điện thoại chỉ gửi lên metadata và preview nhỏ.
// Chỉ ảnh được chọn mới có bản RAW đầy đủ đi kèm.
type ImageRef struct {
	ID       string      `json:"id"`
	Filename string      `json:"filename"`
	Format   ImageFormat `json:"format"`
	ByteSize int64       `json:"byteSize"`
	// CapturedAt là giờ của máy ảnh, có thể lệch giờ điện thoại và giờ server.
	// Đừng dùng nó để sắp xếp tuyệt đối giữa nhiều thiết bị mà không hiệu chỉnh.
	CapturedAt time.Time `json:"capturedAt"`
	IsRaw      bool      `json:"isRaw"`

	// OriginalAvailable cho biết bản RAW đã lên tới backend chưa.
	// False là trạng thái BÌNH THƯỜNG, không phải lỗi — xem ADR 0001.
	OriginalAvailable bool `json:"originalAvailable"`
}

// AssetTier là các phiên bản của một tấm ảnh. Xem references/backend-go.md.
type AssetTier string

const (
	TierThumb    AssetTier = "thumb"    // 256px, lưới ảnh và culling
	TierPreview  AssetTier = "preview"  // ~2MP, JPEG nhúng của RAW, màn hình chỉnh
	TierProxy    AssetTier = "proxy"    // 4MP, zoom kiểm tra nét
	TierOriginal AssetTier = "original" // RAW gốc
	TierExport   AssetTier = "export"   // bản giao khách
)

// PresetVersion đánh version cho preset của người dùng.
//
// Preset là tài sản họ giữ nhiều năm. Mọi thay đổi cấu trúc sau này phải migrate
// được. Đánh version từ bản đầu tiên rẻ hơn nhiều so với thêm vào sau.
const PresetVersion = 1

type Preset struct {
	Version   int                        `json:"version"`
	ID        string                     `json:"id"`
	Name      string                     `json:"name"`
	LUT       *PresetLUT                 `json:"lut,omitempty"`
	Basic     map[string]float64         `json:"basic,omitempty"`
	ToneCurve [][2]float64               `json:"toneCurve,omitempty"`
	HSL       map[string]map[string]float64 `json:"hsl,omitempty"`
}

type PresetLUT struct {
	ID string `json:"id"`
	// Amount 0..1 — cường độ, để làm slider.
	Amount float64 `json:"amount"`
}
