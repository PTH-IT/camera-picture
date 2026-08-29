# Native module tầng capture (iOS)

Cầu nối giữa React Native và SDK máy ảnh.

## Trạng thái từng file

| File | Trạng thái |
|---|---|
| `CaptureSourceModule.swift` | ✅ Hoàn chỉnh — bridging, sự kiện, promise, tuần tự hoá luồng |
| `CaptureSourceModule.m` | ✅ Hoàn chỉnh — khai báo cầu nối cho Swift |
| `CameraBackend.swift` | ✅ Hoàn chỉnh — giao thức và kiểu dữ liệu |
| `MockBackend.swift` | ✅ **Chạy được thật** — máy ảnh giả, dùng ngay được |
| `CascableBackend.swift` | ⚠️ Khung sườn — các chỗ gọi SDK đánh dấu `NEEDS SDK` |
| `CaptureSource.podspec` | ✅ Nối module vào dự án Xcode bằng `pod install` |

Mọi thứ trừ `CascableBackend` đều chạy được ngay và không phụ thuộc SDK nào.

Phía JavaScript đã nối xong: `mobile/src/capture/adapter.ts` giải mã dữ liệu từ
module này, `mobile/src/state/capture.ts` đưa ảnh lên lưới và đẩy metadata lên
máy chủ. Nghĩa là đổi `MockBackend` sang `CascableBackend` là đủ để có tether
thật — không còn phần nào của luồng chờ được viết.

## Vì sao có `MockBackend`

Không phải đồ chơi. Nó cho phép:

- Phát triển và kiểm thử **toàn bộ luồng ứng dụng** trước khi có license
- Chạy trên simulator, nơi không cắm được máy ảnh
- Kiểm thử tự động trên CI mà không cần phần cứng
- **Tái hiện các trường hợp khó dựng bằng máy thật**: rớt kết nối giữa lúc truyền,
  ảnh không có preview nhúng, thẻ đầy

Điểm cuối là quan trọng nhất. Những lỗi tệ nhất của tether xảy ra đúng lúc mọi thứ
không suôn sẻ, và dựng lại chúng bằng máy ảnh thật vừa chậm vừa không lặp lại được.

```swift
let mock = MockBackend()
mock.capabilities.removeAll { $0 == .previewWithoutFullDownload }  // buộc tải cả RAW
mock.simulateDisconnect = true                                      // rớt sau 20 giây
```

## Vì sao `CascableBackend` chưa gọi SDK

Mình không có CascableCore để biên dịch và kiểm chứng. Đoán tên hàm sẽ cho ra một
file **trông như đã xong nhưng sai** — tốn thời gian hơn nhiều so với một chỗ trống
có chỉ dẫn chính xác. Mỗi mục `NEEDS SDK` ghi rõ cần tra cái gì trong
[`cascablecore-demo`](https://github.com/Cascable/cascablecore-demo).

## Thứ tự làm trong 30 ngày dùng thử

Theo mức rủi ro giảm dần, không theo thứ tự file:

**1. `fetchPreview` — làm trước tiên.** Đây là giả định nguy hiểm nhất của toàn bộ
kiến trúc: CascableCore có lấy được JPEG preview nhúng mà **không** tải cả file RAW
không? Nếu không, chiến lược "để ảnh trên thẻ" phải thiết kế lại — và biết ở ngày 2
rẻ hơn rất nhiều so với ngày 25.

**2. Độ phủ body.** Bảng compatibility công khai của Cascable là của **Cascable
Studio**, chưa xác nhận cho SDK. Thử đúng danh sách Z6III / Z5II / Z50II / Zf / ZR.

**3. Đo thời gian bấm máy → preview hiện**, qua cả USB-C lẫn Wi-Fi. Con số này
quyết định sản phẩm có dùng được tại buổi chụp hay không.

## Đưa module vào dự án Xcode

Không kéo file vào Xcode bằng tay. Module là một **pod cục bộ**: `../bootstrap.sh`
sinh dự án và chèn sẵn dòng khai báo vào Podfile,

```ruby
pod 'CaptureSource', :path => './CaptureSource'
```

rồi `pod install` biên dịch cả Swift lẫn Objective-C vào target. Cách này dựng
lại y hệt trên mọi máy và chạy được trên CI; kéo tay thì chỉ đúng trên máy của
người đã kéo, và pull request không kiểm chứng được.

Không cần bridging header: `CaptureSourceModule.m` chỉ dùng macro
`RCT_EXTERN_MODULE`, vốn không đọc header Swift sinh ra.

## Tích hợp SDK

1. Thêm CascableCore qua Swift Package Manager:
   `https://github.com/Cascable/cascablecore-distribution`
2. Bỏ chú thích hai dòng `import` đầu `CascableBackend.swift`
3. Điền các chỗ `NEEDS SDK`
4. Đổi backend trong `CaptureSourceModule.init()`:
   `backend = CascableBackend()` thay cho `MockBackend()`

**Không commit binary hoặc file license của CascableCore.** `.gitignore` đã chặn —
đó là SDK thương mại.

## Quyền cần khai trong `Info.plist`

```xml
<key>NSCameraUsageDescription</key>
<string>Kết nối với máy ảnh của bạn để nhận ảnh trong lúc chụp.</string>
<key>NSLocalNetworkUsageDescription</key>
<string>Kết nối Wi-Fi trực tiếp tới máy ảnh của bạn.</string>
<key>NSBonjourServices</key>
<array><string>_ptp._tcp</string></array>
```

`NSLocalNetworkUsageDescription` và `NSBonjourServices` là **bắt buộc** cho tether
qua Wi-Fi từ iOS 14. Thiếu chúng, việc tìm máy ảnh im lặng không trả kết quả nào —
không có lỗi, không có cảnh báo, chỉ là danh sách rỗng mãi mãi. Đây là một trong
những lỗi tốn thời gian nhất khi làm tether trên iOS.

## Hai luật không được vi phạm

**Pixel không đi qua cầu JavaScript.** Ảnh luôn được trả về dưới dạng `LocalImage`
trỏ tới file trong thư mục tạm. Một NEF là 55MB; đẩy nó qua cầu là giết hiệu năng
và làm OOM app. `MockBackend` cũng tuân thủ đúng điều này — nếu nó trả bytes trực
tiếp, nó sẽ giấu đi chính vấn đề mà luật này tồn tại để tránh.

**Khả năng do SDK báo, không suy từ tên hãng.** Viết
`if manufacturer == "Nikon"` sẽ sai ngay trong nội bộ dòng Z: các body khác nhau
hỗ trợ khác nhau, và cùng một body qua USB với qua Wi-Fi cũng khác nhau. Toàn bộ
giao diện rẽ nhánh theo danh sách khả năng này.
