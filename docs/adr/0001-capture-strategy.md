# ADR 0001 — Chiến lược capture

- **Ngày:** 2026-08-23
- **Trạng thái:** Đã chấp nhận, **có điều kiện** — xem [Rủi ro](#rủi-ro-mở)
- **Đã sửa đổi bởi:** [ADR 0003](0003-capture-fallback.md) — phần "nếu Cascable từ chối" đã lỗi thời
- **Bối cảnh sản phẩm:** app nhiếp ảnh kiểu Evoto — lấy ảnh trực tiếp từ máy ảnh, áp màu riêng, hiển thị ngay, cộng tính năng AI

## Ràng buộc

Ba ràng buộc do phía sản phẩm đưa ra, được coi là cố định:

1. Hãng máy ảnh chính: **Nikon**, phủ gần như toàn bộ dòng Z (Z9, Z8, Z7II/Z7, Z6III/Z6II/Z6, Z5II/Z5, Zf, ZR, Z50II/Z50, Z30, Zfc)
2. **Không dùng thiết bị trung gian** — không laptop, không mini-PC tại buổi chụp
3. **iOS bắt buộc** ở phase 1

Backend Golang, mobile React Native.

## Các phương án đã xét và loại

| Phương án | Lý do loại |
|---|---|
| Hot-folder + NX Tether | Cần máy Windows/macOS — vi phạm ràng buộc 2 |
| Nikon SDK (miễn phí) | Chỉ chạy Windows 11 64-bit và macOS — vi phạm ràng buộc 2 |
| Go agent + libgphoto2 | Cần một máy tính — vi phạm ràng buộc 2 |
| Android USB PTP (libgphoto2 NDK) | Không giải quyết iOS — vi phạm ràng buộc 3 |
| **ImageCaptureCore (Apple)** | Framework của chính Apple, có trên iOS 13+, nhưng **nhiều báo cáo `ICDeviceBrowser` không tìm thấy thiết bị trong app bên thứ ba từ iOS 18**. App Photos của Apple vẫn chạy. iOS 17 trước đó cũng từng phá vỡ USB tethering. |
| Tự implement PTP/IP over WiFi | libgphoto2 còn issue mở với Nikon Z (Z8 trả PTP error 2005; bug treo máy sau truyền host→camera). Chi phí cao, rủi ro rất cao. |
| SnapBridge | **Không hỗ trợ RAW.** Auto-download 2MP, một số Z đời mới 8MP. Không đủ cho sản phẩm RAW chuyên nghiệp. |

## Quyết định

**Dùng CascableCore cho tầng capture trên iOS.**

Lý do: sau khi áp cả ba ràng buộc, đây là phương án khả thi duy nhất còn lại. Không phải lựa chọn tối ưu trong số nhiều lựa chọn — là lựa chọn duy nhất.

Điểm cộng đã kiểm chứng (`compatibility.cascable.se/nikon/`, 2026-08): **toàn bộ dòng Z mục tiêu đều nằm trong danh sách hỗ trợ**, kể cả các body mới nhất (Z6III, Z5II, Z50II, Zf, ZR). Với Z8, hỗ trợ cả USB lẫn WiFi, đầy đủ remote control, tethered shooting, **live view**, storage access, và RAW.

### Chiến lược nền tảng: (a) rồi (b)

- **Phase 1 — iOS:** tether qua CascableCore. Android ra mắt **không có tether**, chỉ xem/chỉnh/AI trên ảnh đã đồng bộ.
- **Phase sau — Android:** thêm tether qua libgphoto2 NDK + USB Host, như một implementation thứ hai của cùng hợp đồng.

CascableCore chỉ có trên nền tảng Apple (iOS, iPadOS, macOS, visionOS), nên không có đường nào dùng chung code capture giữa hai nền tảng.

## Hệ quả

### Hợp đồng `CaptureSource` là ranh giới sống còn

Vì sẽ có implementation thứ hai với tập khả năng khác hẳn, hợp đồng tại
`mobile/src/capture/types.ts` phải được giữ nghiêm:

- **Rẽ nhánh theo `capabilities`, không theo hãng máy hay SDK.** libgphoto2 không có live view với Nikon; CascableCore thì có. Cùng một body, hai đường capture, hai tập khả năng.
- **Pixel không đi qua cầu JS.** Ảnh tham chiếu bằng `ImageHandle` (URI phía native).
- **Chữ "Cascable" không được xuất hiện ngoài native module iOS.** Thấy nó ở tầng trên là rò rỉ trừu tượng, và sẽ thành nợ khi làm Android.

### Kiến trúc lưu trữ: để ảnh trên thẻ

Hệ quả nghiêm trọng nhất của việc bỏ laptop, và không hiển nhiên.

NEF từ Z8 ≈ 50–60MB. Một đám cưới 2000 shot là **hơn 100GB**. Điện thoại không chứa nổi; upload qua mạng cũng không.

Luồng bắt buộc:

1. Duyệt thẻ nhớ, **không tải hết** (`listItems`)
2. Chỉ kéo **JPEG preview nhúng** cho toàn bộ shot — vài trăm MB cho 2000 tấm, đủ để lướt, cull, áp LUT, cho khách xem
3. Chỉ tải **RAW đầy đủ cho ảnh đã chọn**
4. Chỉ upload lên server thứ cần render chất lượng cao hoặc cần AI

Máy ảnh trở thành ổ lưu trữ; điện thoại là màn hình và bộ não. Phải thiết kế từ đầu — không chắp vá được.

### Vai trò backend Go thu hẹp nhưng không mất

Go không còn làm capture. Vẫn giữ: tài khoản và license, đồng bộ đa thiết bị, render RAW chất lượng cao khi xuất (libraw + libvips), điều phối AI, phân phối preset/LUT/Picture Control, lưu trữ dài hạn.

### Live view mở ra tính năng Evoto không có

Vì CascableCore hỗ trợ live view với Nikon, app có thể hiển thị **khung ngắm đã áp LUT của người dùng, trước khi bấm máy**. Evoto là phần mềm hậu kỳ — nó đứng sau khoảnh khắc chụp. Đây là chỗ khác biệt hoá thật.

## Rủi ro mở

Quyết định này được chấp nhận **có điều kiện**, vì hai câu hỏi chưa có lời đáp:

1. **Giá và việc được chấp nhận.** CascableCore có giá thương lượng theo từng ứng dụng, dành cho sản phẩm/công ty đạt "partnership criteria" — **Cascable có quyền từ chối**. Phải liên hệ ngay tuần đầu.

   **Đã hạ mức nghiêm trọng.** Khi viết ADR này, đây là rủi ro số một vì không có
   phương án thay thế. [ADR 0003](0003-capture-fallback.md) cho thấy tự implement
   PTP/IP qua WiFi cho Nikon Z là khả thi — đã có người làm được một mình và đưa
   lên App Store. Cascable từ chối không còn là ngõ cụt.

2. **Bảng compatibility là của Cascable Studio, không phải CascableCore.** Studio xây trên Core nên độ phủ gần trùng, nhưng chưa được xác nhận cho SDK. Phải test đúng danh sách body trong 30 ngày trial.

Rủi ro thứ ba, thuộc kiến trúc:

3. **`previewWithoutFullDownload` là giả định chưa kiểm chứng.** Nếu CascableCore bắt buộc tải cả file RAW mới đọc được preview nhúng, toàn bộ chiến lược "để ảnh trên thẻ" sụp đổ. Đã mô hình hoá thành một `CameraCapability` để app degrade được thay vì vỡ — nhưng phải spike sớm.

## Việc phải làm, theo thứ tự

1. Chuẩn bị body và thiết bị test **trước**, rồi mới kích hoạt trial 30 ngày (đồng hồ chạy ngay khi kích hoạt)
2. **Song song:** liên hệ Cascable về giá và partnership criteria
3. Test đúng danh sách body, đặc biệt Z6III/Z5II/Z50II/Zf/ZR
4. Đo thời gian từ lúc bấm máy đến lúc preview hiện trên điện thoại, qua USB-C và qua WiFi
5. Kiểm chứng `previewWithoutFullDownload`

## Xét lại quyết định này khi nào

- Cascable từ chối cấp license, hoặc giá vượt ngân sách → **tự implement PTP/IP
  qua WiFi cho Nikon Z**, xem [ADR 0003](0003-capture-fallback.md). Không cần nới
  ràng buộc nào — kết luận cũ ("nới 'không laptop'") đã lỗi thời
- `previewWithoutFullDownload` hoá ra không khả thi → thiết kế lại tầng lưu trữ trước khi viết tiếp
- Apple sửa ImageCaptureCore cho app bên thứ ba → xét lại, vì đó là đường miễn phí và chạy được cả trên iPad
