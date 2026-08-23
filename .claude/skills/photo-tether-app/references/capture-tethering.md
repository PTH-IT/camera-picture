# Capture / Tethering — ma trận phương án

Mục lục:
- [Ma trận quyết định](#ma-trận-quyết-định)
- [Phương án A: Go agent trung gian (khuyến nghị cho MVP)](#phương-án-a-go-agent-trung-gian)
- [Phương án B: Canon CCAPI trực tiếp từ RN](#phương-án-b-canon-ccapi)
- [Phương án C: USB PTP trên Android](#phương-án-c-usb-ptp-trên-android)
- [Phương án D: CascableCore cho iOS](#phương-án-d-cascablecore-cho-ios)
- [Ghi chú theo từng hãng](#ghi-chú-theo-từng-hãng)
- [Giao thức nội bộ agent ↔ app](#giao-thức-nội-bộ-agent--app)

## Ma trận quyết định

| Phương án | Android | iOS | Phủ máy ảnh | Cần thiết bị phụ | Chi phí | Rủi ro kỹ thuật |
|---|---|---|---|---|---|---|
| A. Go agent + libgphoto2 | ✅ | ✅ | Rất rộng | Có (laptop/Pi) | Thấp | Thấp |
| B. Canon CCAPI (HTTP) | ✅ | ✅ | Chỉ Canon đời mới | Không | Thấp | Thấp |
| C. USB PTP native Android | ✅ | ❌ | Rộng | Không | Trung bình | Trung bình |
| D. CascableCore SDK | ❌ | ✅ | ~250 model | Không | License thương mại | Thấp |
| E. PTP/IP tự implement | ✅ | ✅ | Hẹp, thất thường | Không | Cao (nhân lực) | **Cao** |
| F. ImageCaptureCore (Apple) | ❌ | ⚠️ | PTP nói chung | Không | Miễn phí | **Cao — hỏng ở iOS 18** |

Nguyên tắc chọn: nếu người dùng chưa xác định rõ hãng máy và chưa chấp nhận rủi ro, **luôn bắt đầu bằng A**. A không loại trừ B/C/D về sau — nếu định nghĩa giao thức agent↔app cho tử tế, việc thêm nguồn capture khác chỉ là thay implementation phía sau cùng một interface.

## Phương án A: Go agent trung gian

Agent là một binary Go chạy trên laptop tại buổi chụp (hoặc mini-PC/Raspberry Pi gắn cố định trong studio). Camera cắm USB vào máy đó. Agent nói chuyện với camera qua **libgphoto2** (cgo), rồi đẩy ảnh về app qua LAN.

Vì sao đây là lựa chọn đúng cho MVP:
- libgphoto2 phủ hầu như mọi máy ảnh còn dùng, đã được tôi luyện nhiều năm.
- Không đụng tới giới hạn USB của iOS.
- Chính là Go — dùng lại được kiến thức và toolchain của team.
- Studio thật vốn đã tether qua laptop, nên không tạo thêm ma sát vận hành.

Điểm cần lưu ý khi implement:
- `libgphoto2` **không thread-safe** trên cùng một `Camera` handle. Serialize mọi thao tác qua một goroutine sở hữu handle, giao tiếp bằng channel.
- Dùng `gp_camera_wait_for_event` để bắt sự kiện ảnh mới, đừng polling thư mục.
- Bật chế độ để camera giữ ảnh trên thẻ nhớ song song với việc truyền — nếu mất kết nối, ảnh vẫn còn.
- Có model chỉ truyền được JPEG khi bật RAW+JPEG; kiểm tra `capturetarget` và `imageformat` config trước khi bắt đầu buổi chụp.
- Trên Linux, `gvfs-gphoto2-volume-monitor` sẽ giành thiết bị. Phải xử lý (unmount hoặc udev rule), nếu không sẽ gặp lỗi "Could not claim the USB device".

Binding Go: có thể gọi trực tiếp libgphoto2 qua cgo, hoặc gọi CLI `gphoto2 --capture-tethered` và parse output. CLI dễ hơn nhiều để bắt đầu và đủ ổn định cho MVP; chuyển sang cgo khi cần kiểm soát chi tiết (live view, đổi setting).

## Phương án B: Canon CCAPI

API HTTP/REST thuần do Canon phát hành chính thức. Đây là đường **dễ nhất tuyệt đối** để tether trực tiếp từ React Native — không cần native module nào, chỉ `fetch`.

- Phải kích hoạt CCAPI trong menu máy (yêu cầu một lần setup từ trình duyệt).
- Cần đăng ký tài khoản Canon Developer Community để lấy tài liệu.
- Phủ Canon đời mới: R5, R5 II, R6, R6 II, R7, R10, R3, 90D, 850D, 250D, RP, M6 II, M50 II, M200, một số PowerShot. **Danh sách này thay đổi — tra lại tại Canon Developer Community, đừng trích từ trí nhớ.**
- Có polling sự kiện, live view, tải ảnh, đổi setting.
- v1.1 trở đi hỗ trợ cả kết nối có dây.

Nếu khách hàng mục tiêu chủ yếu dùng Canon, đây có thể là toàn bộ phase 1 và tiết kiệm hàng tháng công sức.

## Phương án C: USB PTP trên Android

Android cho phép USB Host. Hai đường:

1. **libgphoto2 build cho Android NDK**, gắn với `UsbManager` file descriptor lấy từ Java. Cần patch libusb để nhận fd từ ngoài (libusb có `libusb_wrap_sys_device` cho việc này). Rồi viết native module RN (Kotlin + JNI).
2. **Tự implement PTP over USB bulk transfer** bằng Kotlin. Khả thi cho tập lệnh nhỏ (list, download, capture) nhưng sẽ đau khi gặp vendor extension của Nikon/Canon.

Đường 1 tốn công build ban đầu nhưng thắng về độ phủ. Đường 2 chỉ hợp lý nếu chỉ cần "tải ảnh vừa chụp về" và giới hạn vài model.

Bẫy: Android yêu cầu người dùng cấp quyền USB qua dialog mỗi lần cắm, trừ khi khai `intent-filter` `USB_DEVICE_ATTACHED` với `device_filter.xml` theo vendor/product ID.

## Phương án D: CascableCore

SDK thương mại (`developer.cascable.se`), phủ Canon, Fujifilm, GoPro, Nikon, Olympus/OM System, Panasonic, Sony — khoảng 250+ model, qua cả WiFi lẫn USB, với **một API duy nhất**.

- Đây thực tế là con đường khả dĩ duy nhất cho **USB tethering trên iOS**. Cascable Studio là app hiếm hoi làm được điều này.
- Là SDK iOS/macOS (Swift/ObjC) → phải viết RN native module bọc lại.
- Tại thời điểm khảo sát chưa thấy bản Android — kiểm chứng lại nếu cần.
- Có bản dùng thử đầy đủ 30 ngày để thử trước khi cam kết.
- Nền tảng: iOS, iPadOS, macOS, visionOS. **Không có Android** — nếu app cần Android, tầng capture phải viết lại hoàn toàn.
- Giá thương lượng theo từng ứng dụng, có "partnership criteria" → **họ có thể từ chối**. Liên hệ sớm.

Cân nhắc: chi phí license đổi lấy việc xóa bỏ toàn bộ rủi ro protocol. Nếu iOS là bắt buộc và ngân sách cho phép, đây thường là quyết định đúng về mặt kinh tế.

## Ghi chú theo từng hãng

**Nikon** — hãng chính của dự án này, xem `nikon.md` để có bản đầy đủ đã kiểm chứng.
- SDK chính thức (Nikon SDK) **miễn phí**, chỉ cần đăng ký — nhưng chỉ chạy Windows 11 64-bit và macOS, và Nikon không hỗ trợ kỹ thuật.
- SnapBridge dùng BLE + WiFi với giao thức riêng, không public.
- Dòng Z hỗ trợ PTP/IP qua WiFi, nhưng libgphoto2 vẫn còn issue mở — ví dụ Z8 trả về "PTP error 2005" khi query device properties, và có bug làm máy treo sau khi truyền host→camera. **Đừng cam kết deadline dựa trên đường này.**
- USB MTP/PTP hoạt động tốt và ổn định hơn nhiều so với PTP/IP.

**Canon**
- CCAPI (HTTP) cho máy đời mới — dễ nhất.
- EDSDK cho desktop, cần đăng ký, mạnh và đầy đủ hơn CCAPI.

**Sony**
- Camera Remote SDK (CRSDK) hiện đại: Windows/Linux/macOS, USB + Ethernet. Không có bản mobile.
- Camera Remote API cũ (HTTP/JSON-RPC over WiFi) chỉ còn dùng được với máy đời cũ.

**Fujifilm**
- Có Camera Remote SDK (desktop). PTP/IP đã được reverse-engineer khá tốt trong cộng đồng; tồn tại thư viện Go (`malc0mn/ptp-ip`) tập trung vào Fuji — hữu ích nếu Fuji là ưu tiên.

## Giao thức nội bộ agent ↔ app

> **Lỗi thời với dự án này.** Phần dưới mô tả interface phía Go, đúng cho kiến trúc có agent trung gian. Dự án đã bỏ agent — capture chạy trên điện thoại. Hợp đồng đang dùng là **`mobile/src/capture/types.ts`** trong repo. Giữ mục này lại vì nó vẫn đúng nếu sau này quay lại kiến trúc agent.

Thiết kế lớp này cho tử tế ngay từ đầu — nó là chỗ cách ly mọi khác biệt giữa các phương án capture ở trên.

Interface tối thiểu:

```go
type CaptureSource interface {
    Connect(ctx context.Context) error
    Info() CameraInfo                     // hãng, model, khả năng hỗ trợ
    Events(ctx context.Context) <-chan CaptureEvent
    Download(ctx context.Context, id string, w io.Writer) error
    Settings() (map[string]Setting, error)
    SetSetting(key string, value any) error
    Close() error
}

type CaptureEvent struct {
    Kind     EventKind   // ImageAdded, SettingChanged, Disconnected
    ImageID  string
    Filename string
    Format   string      // "NEF", "CR3", "JPEG"
    Preview  []byte      // JPEG preview nhúng, gửi kèm ngay để app hiện tức thì
}
```

Kênh truyền về app: WebSocket cho event + HTTP range request cho asset. Gửi `Preview` ngay trong event để app hiện ảnh dưới 1 giây; file gốc tải nền sau.

Yêu cầu bắt buộc về độ tin cậy: agent phải có **hàng đợi bền** (ghi xuống đĩa) cho ảnh chưa gửi được. Buổi chụp thật hay mất WiFi, và mất một tấm ảnh của khách là sự cố không thể chấp nhận.
