---
name: photo-tether-app
description: Kiến trúc và triển khai app nhiếp ảnh kiểu Evoto/Capture One — tether ảnh trực tiếp từ máy ảnh (Nikon/Canon/Sony/Fuji), áp LUT/preset màu riêng, hiển thị real-time, và các tính năng AI (retouch da, tách nền, culling, upscale). Stack: backend Golang, mobile React Native, AI sidecar Python. Dùng skill này BẤT CỨ KHI NÀO người dùng nhắc tới tether/tethering, chụp ảnh từ máy ảnh vào app, PTP/PTP-IP/MTP, gphoto2, CCAPI, EDSDK, CascableCore, SnapBridge, file RAW (NEF/CR3/ARW/RAF), LUT/.cube/hald, color grading, preset màu, retouch AI, hoặc khi họ hỏi về kiến trúc/khả thi/lộ trình cho app xử lý ảnh chuyên nghiệp — kể cả khi họ không gọi tên "tethering" ra.
---

# App tether ảnh + color grading + AI

Skill này giữ toàn bộ quyết định kiến trúc và các cạm bẫy đã biết cho dự án ở `G:\camera`.
Mục tiêu: app nhận ảnh trực tiếp từ máy ảnh, áp màu riêng của người dùng, hiển thị gần như tức thì, cộng thêm các tính năng AI cho nhiếp ảnh gia.

**Quyết định đã chốt (2026-08-23):** hãng chính **Nikon** (gần như toàn bộ dòng Z), **không dùng thiết bị trung gian**, **iOS bắt buộc**, capture đi bằng **CascableCore**. Ba ràng buộc này loại sạch các đường rẻ — đừng đề xuất lại agent/hot-folder trừ khi người dùng chủ động nới ràng buộc.

Đọc trước khi tư vấn bất cứ điều gì về capture: **`references/ios-cascablecore.md`** (kiến trúc đã chốt) và **`references/nikon.md`** (đặc thù Nikon, Picture Control).

## Nguyên tắc gốc — đọc trước khi đề xuất bất cứ gì

Bốn điều dưới đây là kết quả của việc phân tích ràng buộc thực tế, không phải sở thích. Khi người dùng đề xuất phương án khác, hãy đối chiếu với chúng và nói rõ đánh đổi thay vì gật đầu:

1. **Phần khó nhất là capture, không phải AI.** AI có model sẵn, có API sẵn. Còn việc lấy được ảnh ra khỏi thân máy ảnh vào một cái điện thoại thì bị chặn bởi Apple, bởi protocol đóng của hãng máy ảnh, và bởi NDA. Mọi ước lượng thời gian phải đặt trọng số ở đây.

2. **Golang không làm inference.** Go giữ vai trò orchestration, I/O, queue, storage, render pipeline (qua cgo tới libraw/libvips). Model AI sống trong service Python riêng, giao tiếp bằng gRPC. Đừng cố nhét ONNX Runtime Go binding cho model phức tạp — sẽ mất nhiều thời gian hơn là được.

3. **Màu apply trên GPU của thiết bị, không round-trip server.** Người dùng kéo slider 60 lần/giây. Mỗi lần gọi server là trải nghiệm chết. Server chỉ render bản final full-res.

4. **Không bao giờ decode RAW để hiển thị preview.** Mọi file RAW đều nhúng sẵn JPEG preview. Dùng nó. Decode RAW chỉ xảy ra ở worker khi xuất bản cuối.

## Kiến trúc chuẩn

```
┌──────────┐   USB/WiFi   ┌───────────────┐
│ Camera   │─────────────▶│ Capture layer │  (xem references/capture-tethering.md)
└──────────┘              └───────┬───────┘
                                  │ WebSocket / HTTP
                          ┌───────▼────────┐        ┌──────────────────┐
                          │  Go API + BE   │◀─gRPC─▶│ Python AI service│
                          │  auth, session │        │  GPU inference   │
                          │  queue, store  │        └──────────────────┘
                          └───────┬────────┘
                                  │ WebSocket (event) + HTTP (asset)
                          ┌───────▼────────┐
                          │ React Native   │  LUT trên GPU bằng Skia RuntimeEffect
                          └────────────────┘
```

## Chọn phương án capture

Đây là quyết định kiến trúc quan trọng nhất. Đừng chọn theo cảm tính — hỏi người dùng 3 câu:

- Máy ảnh nào là chính? (Canon → CCAPI mở đường dễ nhất; Nikon Z → khó nhất)
- Có chấp nhận một thiết bị trung gian (laptop/mini-PC/Raspberry Pi) tại buổi chụp không?
- iOS có bắt buộc ở phase 1 không?

Với dự án này câu trả lời đã chốt: **CascableCore trên iOS.** Lý do và bảng loại trừ đầy đủ nằm trong `references/ios-cascablecore.md`.

Ma trận đầy đủ các phương án, protocol, giới hạn từng hãng, và code pattern: **`references/capture-tethering.md`**. Riêng Nikon: **`references/nikon.md`**.

## Pipeline màu

Luồng hai tốc độ — nhanh cho mắt, chậm cho file:

| | Đường nhanh (device) | Đường chậm (server) |
|---|---|---|
| Nguồn | JPEG preview nhúng trong RAW | RAW gốc |
| Xử lý | Skia `RuntimeEffect` + hald LUT PNG | libraw → libvips → bake LUT + ICC |
| Độ trễ | < 16ms/frame | vài giây/ảnh |
| Dùng khi | Xem, chỉnh, so sánh preset | Xuất, giao khách, in |

Điểm mấu chốt: **cùng một LUT phải cho cùng kết quả ở cả hai đường.** Nếu shader trên máy và libvips trên server lệch nhau, người dùng sẽ mất niềm tin ngay lần xuất file đầu tiên. Phải có test đối chiếu pixel giữa hai đường.

Chi tiết định dạng LUT, code SkSL, xử lý color space, cách tránh lệch màu: **`references/color-pipeline.md`**.

## Tính năng AI

Chia theo nơi chạy, không theo tính năng:

- **On-device** (ONNX Runtime RN / TFLite / Core ML): những thứ chạy khi người dùng đang lướt — phát hiện mặt, chấm điểm nét/nhắm mắt để culling, phân đoạn nền độ phân giải thấp. Rẻ, offline được, riêng tư.
- **Server GPU**: những thứ nặng và chỉ chạy một lần trên ảnh được chọn — retouch da, tách nền chất lượng cao, thay trời, upscale, khử nhiễu RAW.

Danh sách model cụ thể cho từng tính năng, kèm cân nhắc license thương mại: **`references/ai-features.md`**.

## Backend Go

Go không còn làm capture, nhưng vẫn giữ auth, đồng bộ, render RAW khi xuất, điều phối AI, phân phối preset và lưu trữ. Cấu trúc service, job queue, storage, và các thư viện cgo: **`references/backend-go.md`**.

## Khi được hỏi về khả thi hoặc lộ trình

Trả lời trung thực theo khung này, đừng hứa quá:

- MVP (agent Go + một hãng máy + LUT trên Skia + gallery): 8–12 tuần
- Thêm AI cơ bản (retouch da, tách nền, culling): +8–10 tuần
- Direct tether trên Android/iOS không cần agent: +6–10 tuần, **rủi ro cao nhất, có thể thất bại với một số model**

Luôn nêu các rủi ro pháp lý/kỹ thuật đã biết:
- Nikon SDK miễn phí nhưng **chỉ Windows/macOS**, và Nikon không hỗ trợ kỹ thuật. License nằm trong file tải về — phải đọc trước khi thương mại hóa.
- PTP/IP với Nikon Z vẫn còn bug mở trong libgphoto2 (ví dụ PTP error 2005 với Z8) — không đặt timeline dựa trên nó.
- **Live view Nikon:** không có với libgphoto2, **nhưng CÓ với CascableCore**. Luôn hỏi "giới hạn này thuộc đường capture nào" trước khi khẳng định.
- **RAW không chứa nổi trên điện thoại** — một đám cưới Z8 là hơn 100GB. Kiến trúc bắt buộc là "để ảnh trên thẻ, lấy theo nhu cầu".
- **CascableCore không có Android** và giá phải thương lượng — họ có quyền từ chối. Rủi ro dự án số một.
- iOS chặn USB host cho thiết bị PTP tùy ý; nếu bắt buộc có iOS + USB, đường khả dĩ là license CascableCore.
- Chi phí GPU inference là khoản vận hành thật, phải mô hình hóa từ đầu.

## Cạm bẫy hay gặp trong React Native

- **OOM khi mở ảnh lớn.** Không bao giờ đưa ảnh full-res vào JS. Luôn có tầng proxy/thumbnail, và giữ ảnh full-res ở phía native/Skia.
- **Đừng dùng `Image` của RN cho ảnh đang chỉnh màu.** Dùng Skia `Image` + shader, nếu không sẽ không kiểm soát được color pipeline.
- **Filesystem**: ảnh tether đến liên tục, cần chính sách dọn cache rõ ràng, nếu không app sẽ ăn hết bộ nhớ máy sau một buổi chụp.
- **WebSocket reconnect**: buổi chụp thật hay mất WiFi. Phải có hàng đợi ảnh phía agent và resume, không được mất ảnh.

## Kiểm chứng trước khi khẳng định

Danh sách model hỗ trợ của CCAPI, trạng thái bug libgphoto2, và điều khoản SDK các hãng thay đổi theo thời gian. Khi câu trả lời phụ thuộc vào chúng, hãy tra cứu lại thay vì trích từ trí nhớ. Các nguồn đáng tin:

- Canon Developer Community — danh sách máy hỗ trợ CCAPI
- GitHub `gphoto/libgphoto2` — issue tracker cho từng model
- `developer.cascable.se` — CascableCore, xin license đánh giá
- `sdk.nikonimaging.com` — Nikon SDK, danh sách body hỗ trợ và license
- `downloadcenter.nikonimglib.com` — phiên bản NX Tether / NX Studio hiện hành
