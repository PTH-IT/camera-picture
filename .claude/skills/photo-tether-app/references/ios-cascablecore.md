# iOS + CascableCore — đường đã chốt

Quyết định 2026-08-23: dự án giữ cả ba ràng buộc (Nikon, không thiết bị trung gian, iOS bắt buộc) và đi bằng **CascableCore**. File này ghi lại hệ quả kiến trúc.

Mục lục:
- [Độ phủ body đã kiểm chứng](#độ-phủ-body-đã-kiểm-chứng)
- [Đính chính: Nikon CÓ live view qua CascableCore](#đính-chính-nikon-có-live-view-qua-cascablecore)
- [Kiến trúc sau khi bỏ agent](#kiến-trúc-sau-khi-bỏ-agent)
- [Vấn đề lớn nhất: RAW để ở đâu](#vấn-đề-lớn-nhất-raw-để-ở-đâu)
- [Native module cho React Native](#native-module-cho-react-native)
- [Câu chuyện Android](#câu-chuyện-android)
- [Việc phải làm ngay](#việc-phải-làm-ngay)

## Độ phủ body đã kiểm chứng

Tra `compatibility.cascable.se/nikon/` (2026-08). **Toàn bộ dòng Z mục tiêu đều được hỗ trợ:**

| Body | Cascable Studio |
|---|---|
| Z9, Z8, Z7II, Z7, Z6III, Z6II, Z6 | ✅ |
| Z5II, Z5, Zf, ZR | ✅ |
| Z50II, Z50, Z30, Zfc | ✅ |
| D-series (D850, D780, D500, Df, D750...) | ✅ |

Với Z8, trang compatibility ghi rõ: hỗ trợ **cả USB lẫn WiFi**, đầy đủ remote control, tethered shooting, live view, video, photo review, storage access, và **RAW**.

**Cảnh báo quan trọng:** bảng này là của **Cascable Studio** (app tiêu dùng), không phải trực tiếp của **CascableCore** (SDK). Studio được xây trên Core nên độ phủ gần như trùng, nhưng **không được coi là đã xác nhận**. Đây là câu hỏi số một phải làm rõ trong 30 ngày trial: dựng danh sách body của bạn và test từng cái. Cascable cũng nói rõ "exact behaviour and features vary in minor ways between camera models".

## Đính chính: Nikon CÓ live view qua CascableCore

Nhận định trước đó trong skill này — "Nikon không có live view khi tether" — **chỉ đúng với libgphoto2 và các phần mềm tether kiểu Lightroom**, không đúng với CascableCore. Trang compatibility của Z8 liệt kê live view là tính năng được hỗ trợ.

Hệ quả: **được phép thiết kế UI dựa trên live view.** Đây là thay đổi có lợi và mở ra tính năng thật — ví dụ xem trước khung hình đã áp LUT ngay trên điện thoại trước khi bấm máy. Đó là thứ Evoto không làm được, vì Evoto là phần mềm hậu kỳ.

Bất kỳ khẳng định nào về khả năng của Nikon trong skill này đều phải hỏi lại: "điều này đúng với đường capture nào?" Giới hạn của libgphoto2 không phải giới hạn của Nikon.

## Kiến trúc sau khi bỏ agent

```
Nikon Z ──USB-C cable / WiFi──▶ iPhone / iPad
                                    │
                        ┌───────────▼────────────┐
                        │   React Native app     │
                        │  ┌──────────────────┐  │
                        │  │ Swift native mod │  │ ← CascableCore
                        │  │   (capture)      │  │
                        │  ├──────────────────┤  │
                        │  │ Skia RuntimeEff. │  │ ← LUT trên GPU
                        │  └──────────────────┘  │
                        └───────────┬────────────┘
                                    │ WS + HTTP (chỉ ảnh đã chọn)
                        ┌───────────▼────────────┐      ┌──────────────┐
                        │       Go backend       │◀gRPC▶│ Python AI    │
                        │ auth, sync, render RAW │      │ GPU          │
                        │ AI orchestration       │      └──────────────┘
                        │ phân phối preset       │
                        └────────────────────────┘
```

Vai trò Go **thay đổi, không biến mất**. Go không còn làm capture, nhưng vẫn giữ: tài khoản và license, đồng bộ đa thiết bị, render RAW chất lượng cao khi xuất, điều phối AI, phân phối preset/LUT/Picture Control, và lưu trữ dài hạn. Đây vẫn là phần lớn hệ thống.

## Vấn đề lớn nhất: RAW để ở đâu

Đây là hệ quả nghiêm trọng nhất của việc bỏ laptop, và nó **không hiển nhiên**.

Làm phép tính: một file NEF từ Z8 khoảng 50–60MB. Một đám cưới 2000 shot là **hơn 100GB**.

- iPhone không chứa nổi. Ngay cả bản 1TB cũng không hợp lý khi phải chừa chỗ cho các buổi khác.
- Upload 100GB qua mạng di động là không tưởng, và qua WiFi khách sạn cũng vậy.

Nên kiến trúc **bắt buộc** phải là "để ảnh trên thẻ, lấy theo nhu cầu":

1. **Duyệt thẻ nhớ, không tải hết.** CascableCore có storage access — liệt kê file trên thẻ mà không copy.
2. **Chỉ kéo về JPEG preview nhúng** cho toàn bộ shot. Vài trăm KB mỗi tấm → 2000 tấm chỉ vài trăm MB. Đủ để lướt, cull, áp LUT, cho khách xem.
3. **Chỉ tải RAW đầy đủ cho ảnh đã chọn** — thường vài chục đến vài trăm tấm.
4. **Chỉ upload lên server những gì cần render chất lượng cao hoặc cần AI.**

Điều này biến "để ảnh trên thẻ" từ một hạn chế thành một tính năng: máy ảnh trở thành ổ lưu trữ, điện thoại là màn hình và bộ não. Nhưng nó phải được thiết kế từ ngày đầu — không thể chắp vá sau.

Kèm theo: chính sách dọn cache trên điện thoại phải nghiêm ngặt và minh bạch với người dùng, nếu không app sẽ bị gỡ sau buổi chụp thứ hai.

## Native module cho React Native

CascableCore là SDK Swift/ObjC, phân phối qua Swift Package Manager (`Cascable/cascablecore-distribution`), có bản API Swift thuần (`cascablecore-swift`) và app mẫu (`cascablecore-demo`).

Cách bọc:
- Viết **Turbo Module** (New Architecture) bằng Swift. Không dùng bridge cũ — luồng ảnh và sự kiện liên tục cần hiệu năng và kiểu dữ liệu chặt.
- **Giữ ảnh ở phía native.** Đừng bao giờ đẩy buffer ảnh qua cầu JS. JS chỉ nhận ID và metadata; pixel đi thẳng từ native sang Skia.
- **Live view stream**: xử lý hoàn toàn ở native, render qua Skia. Truyền frame qua JS sẽ giết hiệu năng.
- Giữ interface `CaptureSource` làm ranh giới. Hợp đồng thật nằm ở **`mobile/src/capture/types.ts`** trong repo — đọc file đó, không phải bản phác trong `capture-tethering.md` (bản đó thuộc kiến trúc agent cũ, đã bỏ).

Quyền iOS cần khai: `NSCameraUsageDescription`. Kiểm tra thêm yêu cầu riêng của CascableCore trong tài liệu SDK.

## Câu chuyện Android

**CascableCore không có bản Android.** Chỉ iOS, iPadOS, macOS, visionOS.

Ba lựa chọn, phải quyết sớm vì nó ảnh hưởng cách tổ chức code ngay từ đầu:

1. **iOS-only cho tính năng tether, Android ra sau hoặc không có.** Rẻ nhất. Android vẫn dùng được app cho phần xem/chỉnh/AI với ảnh đã đồng bộ, chỉ không tether được.
2. **Android đi libgphoto2 NDK + USB Host.** Implementation hoàn toàn riêng, thừa hưởng toàn bộ bug Nikon Z của libgphoto2. Chi phí thật, đừng đánh giá thấp.
3. **Android đi SnapBridge** (người dùng transfer bằng app Nikon, app bạn đọc từ thư viện ảnh). Rẻ nhưng chỉ 2MP/8MP JPEG, không RAW.

Dù chọn gì, **interface `CaptureSource` phải được định nghĩa trước khi viết native module iOS**, để lựa chọn Android sau này không phải viết lại tầng trên.

## Việc phải làm ngay

Theo thứ tự, không đảo:

1. **Đăng ký trial 30 ngày CascableCore** tại `developer.cascable.se`. Đồng hồ bắt đầu chạy khi bạn kích hoạt — nên chuẩn bị body và thiết bị test trước rồi mới kích hoạt.
2. **Liên hệ Cascable về giá và partnership criteria song song.** Họ có quyền từ chối. Biết điều này ở tuần 1 rẻ hơn nhiều so với ở tháng 6. Đây là rủi ro dự án số một cho tới khi có câu trả lời.
3. **Trong trial, test đúng danh sách body của bạn** — đặc biệt các body mới (Z6III, Z5II, Z50II, Zf, ZR) mà bảng compatibility chỉ xác nhận cho Studio, chưa xác nhận cho Core.
4. **Đo tốc độ thật:** thời gian từ lúc bấm máy đến lúc preview hiện trên điện thoại, qua USB-C và qua WiFi. Con số này quyết định sản phẩm có dùng được tại buổi chụp hay không.
5. **Kiểm chứng luồng "duyệt thẻ, không tải hết"** — CascableCore có cho liệt kê file và chỉ lấy preview nhúng không? Nếu bắt buộc phải tải cả file mới đọc được preview, toàn bộ chiến lược lưu trữ ở trên phải thiết kế lại.

Mục 5 là giả định ngầm nguy hiểm nhất trong kiến trúc hiện tại. Kiểm tra nó sớm.
