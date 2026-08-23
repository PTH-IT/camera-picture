# Tính năng AI — chọn model, chọn nơi chạy

Mục lục:
- [Nguyên tắc phân chia on-device / server](#nguyên-tắc-phân-chia-on-device--server)
- [Bảng tính năng và model](#bảng-tính-năng-và-model)
- [Culling — tính năng bị đánh giá thấp nhất](#culling--tính-năng-bị-đánh-giá-thấp-nhất)
- [Auto color match — không cần AI](#auto-color-match--không-cần-ai)
- [Kiến trúc service AI](#kiến-trúc-service-ai)
- [License — đọc trước khi chọn model](#license--đọc-trước-khi-chọn-model)
- [Chi phí](#chi-phí)

## Nguyên tắc phân chia on-device / server

Chia theo **tần suất chạy**, không theo độ khó của model:

- Chạy trên **mọi** ảnh, ngay khi ảnh đến, trong lúc người dùng đang lướt → **on-device**. Không thể trả tiền GPU cho 2000 ảnh của một buổi cưới.
- Chạy **một lần** trên ảnh người dùng đã chọn, người dùng chấp nhận đợi vài giây → **server GPU**.

Runtime on-device: `onnxruntime-react-native`, TFLite (Android), Core ML (iOS). Với model nhỏ, ONNX Runtime là lựa chọn gọn nhất vì dùng chung một file model cho cả hai nền tảng.

## Bảng tính năng và model

| Tính năng | Nơi chạy | Hướng tiếp cận | Ghi chú |
|---|---|---|---|
| Phát hiện mặt + landmark | Device | MediaPipe FaceMesh / BlazeFace | Nền tảng cho mọi thứ liên quan chân dung |
| Phân đoạn khuôn mặt (da, mắt, môi, tóc) | Device (nhẹ) / Server (chính xác) | BiSeNet face-parsing | Cần cho retouch có kiểm soát |
| Làm mịn da | Server | Frequency separation cổ điển + guided filter, có model hỗ trợ tách texture/màu | **Đừng dùng GAN làm bước chính** — nó thay đổi danh tính khuôn mặt, nhiếp ảnh gia sẽ từ chối |
| Xóa mụn/khuyết điểm | Server | Inpainting (LaMa) giới hạn trong mask da | |
| Làm trắng răng, sáng mắt | Device hoặc Server | Chỉnh màu trong mask từ face parsing | Không cần model riêng |
| Tách nền | Server (chất lượng) / Device (nháp) | BiRefNet, RMBG-2.0, SAM2 | BiRefNet cho biên tóc tốt nhất hiện nay |
| Thay trời | Server | Segmentation bầu trời + hòa màu tiền cảnh | Bước hòa màu quan trọng hơn bước tách |
| Khử nhiễu | Server | NAFNet / SCUNet; hoặc khử nhiễu ngay ở bước demosaic của libraw | Khử ở miền RAW cho kết quả tốt hơn hẳn |
| Upscale | Server | Real-ESRGAN | Cẩn thận với khuôn mặt — dùng bản chuyên biệt |
| Phục hồi khuôn mặt | Server | GFPGAN / CodeFormer | Chỉ dùng cho ảnh hỏng nặng, luôn có slider độ mạnh |
| Culling (chọn ảnh) | Device | Xem mục riêng bên dưới | Giá trị thực tế cao nhất |
| Auto color match | Server (CPU) | Thống kê LAB / Reinhard | Xem mục riêng — không cần AI |

## Culling — tính năng bị đánh giá thấp nhất

Nhiếp ảnh gia cưới về nhà với 3000 ảnh và mất nhiều giờ chỉ để loại bỏ ảnh hỏng. Đây là chỗ tiết kiệm thời gian lớn nhất, và nó **không cần model nặng**:

- **Độ nét**: variance của Laplacian, tính riêng trong vùng mặt chứ không phải toàn ảnh. Ảnh chân dung nền bokeh sẽ bị chấm điểm sai nếu tính toàn khung.
- **Nhắm mắt**: eye aspect ratio từ landmark của FaceMesh. Rẻ, chính xác.
- **Ảnh trùng**: embedding nhẹ (MobileNet/CLIP nhỏ) + phân cụm theo thời gian chụp. Nhóm các shot liên tiếp, đề xuất bản tốt nhất mỗi nhóm.
- **Biểu cảm**: tùy chọn, chấm điểm cười — nhưng để người dùng quyết định, đừng tự loại.

Tất cả chạy được on-device, offline, ngay khi ảnh tether về. Đây nên là tính năng AI đầu tiên được làm.

## Auto color match — không cần AI

"Áp màu của ảnh mẫu lên loạt ảnh này" là nhu cầu rất thật, và giải pháp thống kê cổ điển hoạt động tốt hơn nhiều người tưởng:

- Chuyển sang không gian LAB (hoặc Lαβ), khớp mean và standard deviation từng kênh giữa ảnh nguồn và ảnh tham chiếu (phương pháp Reinhard). Vài chục dòng code, chạy trên CPU, tức thì.
- Nâng cấp: khớp histogram theo từng kênh, hoặc học một LUT 3D từ cặp before/after của chính người dùng — cho phép họ "dạy" hệ thống phong cách riêng từ những ảnh họ đã chỉnh tay.

Việc học LUT 3D từ cặp ảnh là tính năng khác biệt hóa mạnh và rẻ hơn nhiều so với gọi model generative.

## Kiến trúc service AI

```
Go API ──gRPC──▶ Python AI service (FastAPI/grpc, GPU)
                   ├── model registry: nạp sẵn, giữ trong VRAM
                   ├── batching: gom request để tận dụng GPU
                   └── trả về mask/ảnh qua object storage, không qua gRPC payload
```

Quyết định quan trọng: **không truyền ảnh qua gRPC payload.** Go upload ảnh lên object storage, gửi cho Python một URL/key, Python ghi kết quả ngược lại storage và trả về key. Giữ được kích thước message nhỏ và cho phép hai service scale độc lập.

Job dài phải bất đồng bộ: Go nhận request → đẩy vào queue → trả `job_id` → app theo dõi qua WebSocket. Đừng để HTTP request treo 30 giây.

## License — đọc trước khi chọn model

Đây là chỗ dễ vấp khi thương mại hóa:
- **GFPGAN, Real-ESRGAN**: license cho phép dùng thương mại, nhưng kiểm tra lại từng repo và cả model weights (weights đôi khi có license khác code).
- **CodeFormer**: license phi thương mại ở một số bản phát hành — kiểm tra kỹ.
- **RMBG-2.0**: có điều khoản hạn chế thương mại tùy phiên bản.
- **SAM2**: license Apache-ish, nhưng xác nhận lại theo bản phát hành.

Nguyên tắc: trước khi đưa model vào production, đọc license của **cả code lẫn weights**, và ghi lại kết luận vào repo. Đừng dựa vào trí nhớ hay bài blog.

## Chi phí

Mô hình hóa từ đầu, vì đây là khoản vận hành lớn nhất:
- Một GPU inference server (T4/L4) xử lý được bao nhiêu ảnh/giờ cho từng tính năng — đo thật, đừng ước lượng.
- Ảnh nào bắt buộc qua GPU, ảnh nào không. Nếu culling và color match chạy on-device/CPU, phần lớn khối lượng công việc không tốn GPU.
- Cân nhắc cho người dùng chọn: xử lý nhanh (server) hay chậm nhưng miễn phí (on-device, chỉ với tính năng nhẹ).
