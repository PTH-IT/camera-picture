package protocol

import "time"

// Giao thức đồng bộ giữa app và backend.
//
// Điều kiện thiết kế quan trọng nhất, kéo theo mọi thứ khác: PHẦN LỚN ẢNH KHÔNG
// BAO GIỜ LÊN SERVER. Một đám cưới là 2000 shot, hơn 100GB NEF nằm trên thẻ nhớ.
// Server nhận metadata cho tất cả (nhỏ, vài trăm byte mỗi ảnh) và file thật cho
// số ít ảnh được chọn.
//
// Vì vậy giao thức được tối ưu cho "đẩy hàng nghìn bản ghi metadata một cách rẻ
// và idempotent", chứ không phải cho việc truyền file.

// BatchImagesRequest đẩy metadata một lô ảnh lên server.
//
// Idempotent theo ClientID: gửi lại cùng một lô sau khi mất mạng là an toàn và
// không tạo bản trùng. Đây không phải tính năng phụ — buổi chụp thật hay rớt
// mạng, và client buộc phải retry mù.
type BatchImagesRequest struct {
	Images []ImageInput `json:"images"`
}

type ImageInput struct {
	// ClientID bắt nguồn từ item id trên thẻ nhớ. Ổn định giữa các lần gửi lại.
	ClientID   string         `json:"clientId"`
	Filename   string         `json:"filename"`
	Format     ImageFormat    `json:"format"`
	ByteSize   int64          `json:"byteSize"`
	CapturedAt time.Time      `json:"capturedAt"`
	IsRaw      bool           `json:"isRaw"`
	// CameraID là id máy ảnh do máy chủ cấp (POST /v1/cameras), KHÔNG phải id
	// tạm thời của phiên kết nối. Rỗng là hợp lệ; id của người khác thì bị từ chối.
	CameraID   string         `json:"cameraId,omitempty"`
	EXIF       map[string]any `json:"exif,omitempty"`
}

type BatchImagesResponse struct {
	// Ánh xạ ClientID -> id do server cấp, để client cập nhật bảng cục bộ.
	IDs map[string]string `json:"ids"`
	// Created và Updated tách riêng để client biết lô vừa gửi có thật sự thay
	// đổi gì không — hữu ích khi chẩn đoán vòng lặp retry.
	Created int `json:"created"`
	Updated int `json:"updated"`
	// Revision của session sau khi ghi. Client dùng làm con trỏ cho lần pull sau.
	Revision int64 `json:"revision"`
}

// ChangesRequest kéo delta. Client gửi revision nó đã thấy; server trả về mọi
// thứ mới hơn.
//
// Con trỏ là SỐ NGUYÊN LOGIC do server cấp, không phải timestamp. Đồng hồ máy
// ảnh, điện thoại và server đều lệch nhau; và hai thay đổi trong cùng một mili
// giây sẽ khiến con trỏ kiểu timestamp bỏ sót bản ghi một cách âm thầm.
type ChangesRequest struct {
	Since int64 `json:"since"`
	Limit int   `json:"limit,omitempty"`
}

type ChangesResponse struct {
	Images []ImageRecord `json:"images"`
	Edits  []EditRecord  `json:"edits"`
	// Revision để dùng làm `Since` cho lần gọi tiếp theo.
	Revision int64 `json:"revision"`
	// HasMore = true nghĩa là còn bản ghi vượt quá Limit; gọi lại ngay với
	// Since = Revision. Không tự động gộp phía server để một buổi chụp lớn
	// không tạo ra một response khổng lồ giữ trong bộ nhớ.
	HasMore bool `json:"hasMore"`
}

type ImageRecord struct {
	ID         string      `json:"id"`
	ClientID   string      `json:"clientId"`
	Filename   string      `json:"filename"`
	Format     ImageFormat `json:"format"`
	ByteSize   int64       `json:"byteSize"`
	CapturedAt time.Time   `json:"capturedAt"`
	IsRaw      bool        `json:"isRaw"`

	// CameraID rỗng là bình thường: ảnh nhập từ nguồn khác, hoặc client cũ chưa
	// đăng ký máy ảnh.
	CameraID string `json:"cameraId,omitempty"`

	// Assets có thể RỖNG, và đó là trạng thái bình thường: ảnh vẫn nằm trên thẻ.
	Assets map[AssetTier]AssetRecord `json:"assets,omitempty"`

	Revision  int64     `json:"revision"`
	Deleted   bool      `json:"deleted"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type AssetRecord struct {
	StorageKey string `json:"storageKey"`
	ByteSize   int64  `json:"byteSize"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
}

// EditRecord là chỉnh sửa không phá huỷ của một ảnh.
type EditRecord struct {
	ImageID   string         `json:"imageId"`
	PresetID  string         `json:"presetId,omitempty"`
	Overrides map[string]any `json:"overrides,omitempty"`
	Rating    int            `json:"rating"`
	Flagged   bool           `json:"flagged"`
	Rejected  bool           `json:"rejected"`

	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updatedAt"`
	// UpdatedByDevice ghi lại nguồn ghi cuối. Giải quyết xung đột hiện là
	// last-write-wins; trường này để chẩn đoán khi người dùng báo mất chỉnh sửa.
	UpdatedByDevice string `json:"updatedByDevice,omitempty"`
}

// PutEditRequest ghi chỉnh sửa cho một ảnh.
type PutEditRequest struct {
	PresetID  string         `json:"presetId,omitempty"`
	Overrides map[string]any `json:"overrides,omitempty"`
	Rating    int            `json:"rating"`
	Flagged   bool           `json:"flagged"`
	Rejected  bool           `json:"rejected"`
	DeviceID  string         `json:"deviceId,omitempty"`
}

// UploadURLRequest xin link upload cho một tier của ảnh.
//
// File KHÔNG đi qua Go API. Client upload thẳng lên object storage bằng
// presigned URL, rồi báo lại. Cho một NEF 60MB chảy qua handler sẽ giữ goroutine
// và băng thông của API server suốt thời gian upload — với hàng chục client
// đang tether cùng lúc thì đó là cách làm sập service.
type UploadURLRequest struct {
	Tier     AssetTier `json:"tier"`
	ByteSize int64     `json:"byteSize"`
}

type UploadURLResponse struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Headers    map[string]string `json:"headers,omitempty"`
	StorageKey string            `json:"storageKey"`
	ExpiresAt  time.Time         `json:"expiresAt"`
}

// ConfirmAssetRequest báo cho server biết upload đã xong.
type ConfirmAssetRequest struct {
	Tier       AssetTier `json:"tier"`
	StorageKey string    `json:"storageKey"`
	ByteSize   int64     `json:"byteSize"`
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
}

// ErrorResponse là dạng lỗi thống nhất của API.
//
// Code là chuỗi ổn định để client xử lý được mà không phải parse Message —
// Message dành cho con người đọc và có thể đổi bất cứ lúc nào.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const (
	ErrCodeNotFound     = "not_found"
	ErrCodeInvalidInput = "invalid_input"
	ErrCodeConflict     = "conflict"
	ErrCodeInternal     = "internal"
	ErrCodeUnauthorized = "unauthorized"
	// ErrCodeForbidden khác ErrCodeNotFound một cách CÓ CHỦ Ý ở tầng nội bộ,
	// nhưng handler trả 404 cho tài nguyên của người khác — xem requireOwner.
	ErrCodeForbidden = "forbidden"
	// ErrCodeLinkRequired để client hiển thị đúng hướng dẫn: email đã có tài
	// khoản, cần đăng nhập bằng cách cũ rồi liên kết tường minh.
	ErrCodeLinkRequired = "link_required"
	// ErrCodeNotConfigured: tính năng chưa được bật trên bản triển khai này.
	// Khác với lỗi máy chủ — client nên ẩn nút thay vì báo lỗi.
	ErrCodeNotConfigured = "not_configured"
	ErrCodeQuotaExceeded = "quota_exceeded"
	// ErrCodeNotLinked để giao diện hiển thị đúng hành động cần làm ("liên kết
	// lại Drive"), thay vì một thông báo chung mà người dùng không xử lý được.
	ErrCodeNotLinked = "not_linked"
)

// RegisterCameraRequest ghi nhận một thân máy vào tài khoản.
//
// Gọi khi kết nối được máy ảnh, trước khi đẩy ảnh. Trả về id máy chủ cấp; id đó
// mới là thứ đi kèm mỗi ảnh, chứ không phải id tạm của phiên kết nối.
type RegisterCameraRequest struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Firmware     string `json:"firmware,omitempty"`
	// "usb" hoặc "wifi".
	Transport string `json:"transport"`
	// Khả năng do ĐƯỜNG CAPTURE báo, không suy từ tên hãng. Lưu lại để biết ảnh
	// này đến từ nguồn nào và tin được tới đâu — cùng một thân máy qua
	// CascableCore và qua libgphoto2 có tập khả năng khác hẳn nhau.
	Capabilities []string `json:"capabilities,omitempty"`
}
