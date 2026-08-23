# camera-picture

App nhiếp ảnh: lấy ảnh trực tiếp từ máy ảnh Nikon, áp màu riêng, hiển thị ngay, cộng tính năng AI.

## Quyết định đã chốt

| | |
|---|---|
| Hãng máy ảnh | **Nikon**, gần như toàn bộ dòng Z |
| Thiết bị trung gian | **Không** — không laptop, không mini-PC tại buổi chụp |
| Nền tảng phase 1 | **iOS** (tether), Android chỉ xem/chỉnh/AI |
| Tầng capture | **CascableCore** (iOS) → libgphoto2 NDK (Android, phase sau) |
| Backend | Go |
| Mobile | React Native |
| AI | Python sidecar qua gRPC |

Lý do đầy đủ, các phương án đã loại, và rủi ro mở: **[ADR 0001](docs/adr/0001-capture-strategy.md)**.

Quy ước nhánh, commit, và những thứ không được commit: **[CONTRIBUTING.md](CONTRIBUTING.md)**.

## Ba luật không được vi phạm

Vi phạm bất kỳ luật nào sẽ khiến việc thêm Android trở thành viết lại app, chứ không phải thêm một implementation.

1. **Rẽ nhánh theo `capabilities`, không theo hãng máy hay SDK.** libgphoto2 không có live view với Nikon; CascableCore thì có. Cùng một body, hai đường capture, hai tập khả năng.
2. **Pixel không đi qua cầu JS.** Ảnh tham chiếu bằng `ImageHandle` (URI phía native). Một NEF là 50–60MB.
3. **Chữ "Cascable" không xuất hiện ngoài native module iOS.** Thấy nó ở tầng trên là rò rỉ trừu tượng.

## Kiến trúc lưu trữ: để ảnh trên thẻ

Hệ quả quan trọng nhất của việc bỏ laptop. NEF từ Z8 ≈ 50–60MB; một đám cưới 2000 shot là **hơn 100GB** — điện thoại không chứa nổi, upload qua mạng cũng không.

```
duyệt thẻ (không tải)  →  chỉ lấy preview nhúng  →  chỉ tải RAW cho ảnh đã chọn
                                                  →  chỉ upload thứ cần render/AI
```

Máy ảnh là ổ lưu trữ; điện thoại là màn hình và bộ não.

## Cấu trúc

```
mobile/src/capture/     Hợp đồng capture — ranh giới quan trọng nhất
  types.ts              CaptureSource, CameraCapability, ImageHandle
  NativeCaptureSource.ts Spec Turbo Module (lớp vận chuyển, cố ý nghèo nàn)
  index.ts              Xử lý trường hợp Android chưa có tether
backend/
  cmd/api/              HTTP + WebSocket server
  internal/protocol/    Bản phản chiếu Go của hợp đồng TS
docs/adr/               Architecture decision records
.claude/skills/photo-tether-app/   Kiến thức miền đã kiểm chứng
```

`.claude/skills/photo-tether-app/` chứa nghiên cứu đã kiểm chứng về Nikon SDK, libgphoto2, CascableCore, pipeline màu, và model AI. Claude Code tự đọc khi cần; con người cũng đọc được.

## Trạng thái

Mới ở giai đoạn scaffold. **Chưa có gì chạy được** — chủ ý như vậy: 5 việc trong danh sách dưới phải xong trước khi viết tiếp, vì kết quả của chúng có thể thay đổi kiến trúc.

### Việc phải làm, theo thứ tự

1. Chuẩn bị body và thiết bị test **trước**, rồi mới kích hoạt **trial 30 ngày CascableCore** (`developer.cascable.se`) — đồng hồ chạy ngay khi kích hoạt
2. **Song song:** liên hệ Cascable về giá và partnership criteria — **họ có quyền từ chối**, đây là rủi ro dự án số một
3. Test đúng danh sách body, đặc biệt Z6III / Z5II / Z50II / Zf / ZR — bảng compatibility công khai mới chỉ xác nhận cho Cascable Studio, chưa cho SDK
4. Đo thời gian từ lúc bấm máy đến lúc preview hiện trên điện thoại, qua USB-C và qua WiFi
5. Kiểm chứng `previewWithoutFullDownload` — nếu CascableCore bắt buộc tải cả file RAW mới đọc được preview nhúng, **toàn bộ chiến lược lưu trữ phải thiết kế lại**

## Yêu cầu môi trường

Máy hiện tại có Node 20.20.2 và Python 3.11.15. **Go chưa được cài** — cần Go 1.23+ để build backend.

```bash
winget install GoLang.Go
```

## Giấy phép

Proprietary — Copyright (c) 2026 PTH-IT. All rights reserved. Xem [LICENSE](LICENSE).

Giấy phép của **thư viện bên thứ ba không bị thay thế** bởi thông báo này. CascableCore cần license thương mại riêng; model AI có bản hạn chế thương mại. Phải rà soát trước khi phát hành — xem [ADR 0001](docs/adr/0001-capture-strategy.md) và `ai-features.md` trong skill.
