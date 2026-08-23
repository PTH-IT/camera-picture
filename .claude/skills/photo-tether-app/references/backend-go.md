# Backend Go — cấu trúc, thư viện, và các quyết định hạ tầng

Mục lục:
- [Vai trò của Go trong hệ thống](#vai-trò-của-go-trong-hệ-thống)
- [Cấu trúc thư mục](#cấu-trúc-thư-mục)
- [Thư viện](#thư-viện)
- [Job queue và worker](#job-queue-và-worker)
- [Storage và phân tầng ảnh](#storage-và-phân-tầng-ảnh)
- [Realtime tới app](#realtime-tới-app)
- [Mô hình dữ liệu tối thiểu](#mô-hình-dữ-liệu-tối-thiểu)
- [cgo — những điều phải biết](#cgo--những-điều-phải-biết)

## Vai trò của Go trong hệ thống

Go làm bốn việc, và làm rất tốt:
1. **API + realtime**: auth, session chụp, WebSocket đẩy sự kiện ảnh mới.
2. **Orchestration**: điều phối job giữa worker render và service AI.
3. **Render pipeline**: RAW → LUT → xuất, qua cgo tới libraw/libvips.
4. **Agent tether**: binary chạy tại studio, nói chuyện với camera.

Go **không** làm inference model. Xem `ai-features.md`.

## Cấu trúc thư mục

```
cmd/
  api/            # HTTP + WebSocket server
  worker/         # render worker, tiêu thụ job queue
  agent/          # binary tether chạy tại studio
  lutconv/        # .cube -> hald PNG, dùng chung cho app và server
internal/
  capture/        # interface CaptureSource + các implementation
    gphoto/       # cgo libgphoto2
    ccapi/        # Canon HTTP client
  imaging/
    raw/          # libraw binding, trích preview nhúng
    lut/          # parse .cube, sinh hald, áp LUT
    render/       # libvips pipeline
  preset/         # schema preset có version, migration
  jobs/           # định nghĩa job, queue client
  ai/             # gRPC client tới Python service
  store/          # object storage, metadata DB
  api/            # handler, middleware, WebSocket hub
pkg/
  protocol/       # kiểu dữ liệu chung agent <-> api <-> app
```

`pkg/protocol` được dùng bởi cả agent lẫn api — giữ nó không phụ thuộc gì ngoài stdlib để agent build được cho nhiều nền tảng dễ dàng.

## Thư viện

| Việc | Lựa chọn | Ghi chú |
|---|---|---|
| HTTP router | `chi` hoặc stdlib `net/http` (Go 1.22+) | stdlib giờ đã đủ dùng |
| WebSocket | `coder/websocket` (trước là nhooyr) | API sạch, context-aware |
| Xử lý ảnh | `davidbyttow/govips` (libvips) | Streaming, ít RAM, có ICC |
| RAW | libraw qua cgo, hoặc subprocess `darktable-cli` | Subprocess đủ cho MVP |
| Tether USB | libgphoto2 qua cgo, hoặc CLI `gphoto2` | CLI đủ cho MVP |
| Queue | `riverqueue/river` (Postgres) hoặc `hibiken/asynq` (Redis) | River tốt nếu đã có Postgres |
| DB | Postgres + `pgx` | |
| Migration | `golang-migrate` hoặc `goose` | |
| gRPC tới AI | `google.golang.org/grpc` | |
| Config | `caarlos0/env` hoặc `koanf` | |
| Log | `log/slog` (stdlib) | |

Ưu tiên stdlib và thư viện nhỏ. Dự án này đã đủ phức tạp ở tầng imaging, đừng thêm phức tạp ở tầng web.

## Job queue và worker

Các loại job:
- `ingest`: nhận ảnh từ agent, trích preview, sinh thumbnail, ghi metadata.
- `render`: áp preset lên RAW, xuất bản final.
- `ai.*`: gọi service Python, chờ kết quả, cập nhật asset.
- `export`: đóng gói loạt ảnh, sinh link tải.

Yêu cầu:
- **Idempotent.** Worker sẽ retry. Job render chạy hai lần phải cho cùng kết quả và không tạo file rác.
- **Ưu tiên theo hàng đợi riêng.** `ingest` phải luôn nhanh — nó chặn trải nghiệm người dùng. Đừng để nó xếp sau 500 job `export`.
- **Giới hạn song song theo tài nguyên.** libvips và libraw ăn RAM. Đặt trần worker theo RAM khả dụng, không theo số CPU.

## Storage và phân tầng ảnh

Mỗi ảnh tồn tại ở nhiều phiên bản. Định nghĩa rõ và sinh chúng ở bước `ingest`:

| Tầng | Kích thước | Dùng cho | Sinh khi nào |
|---|---|---|---|
| `thumb` | 256px | Lưới ảnh, culling | Ingest, ngay |
| `preview` | ~2MP (JPEG nhúng của RAW) | Màn hình chỉnh sửa | Ingest, ngay — chỉ là trích xuất, gần như miễn phí |
| `proxy` | 4MP | Zoom, kiểm tra nét | Ingest, nền |
| `original` | RAW gốc | Render final, lưu trữ | Upload từ agent |
| `export` | Tùy yêu cầu | Giao khách | Theo yêu cầu |

Object storage (S3/R2/MinIO) cho asset, Postgres chỉ giữ metadata và key. Dùng presigned URL để app tải trực tiếp từ storage — đừng để ảnh chảy qua Go API, sẽ tốn băng thông và chặn goroutine.

## Realtime tới app

WebSocket hub với các sự kiện:
```
session.started        camera.connected     camera.disconnected
image.captured         (kèm thumb + preview URL, gửi ngay)
image.processed        (proxy sẵn sàng)
job.progress           job.completed        job.failed
preset.applied
```

Nguyên tắc: sự kiện `image.captured` phải rời server **trước** khi ảnh gốc upload xong. Người dùng cần thấy ảnh trong vòng một giây sau khi bấm máy — đó là cả điểm hấp dẫn của tether. Preview nhỏ đi kèm event, file lớn theo sau.

Reconnect: app phải gửi kèm `last_event_id` khi kết nối lại, server replay các event bị lỡ. Buổi chụp thật hay rớt mạng.

## Mô hình dữ liệu tối thiểu

```
users
sessions        (một buổi chụp: tên, thời gian, khách hàng)
cameras         (thiết bị đã ghép nối, hãng, model, capability)
images          (session_id, tên file, format, captured_at, EXIF, storage keys)
presets         (user_id, JSON có version — xem color-pipeline.md)
image_edits     (image_id, preset_id, các override cụ thể cho ảnh này)
jobs            (loại, trạng thái, payload, kết quả)
ai_results      (image_id, loại, mask/asset key, tham số model)
```

`image_edits` tách khỏi `images` là quan trọng: chỉnh sửa phải **không phá hủy**. Người dùng phải quay lại được ảnh gốc bất cứ lúc nào, và phải so sánh được các preset khác nhau trên cùng một ảnh.

## cgo — những điều phải biết

Dùng cgo là bắt buộc cho libraw/libvips/libgphoto2, nhưng nó thay đổi cách build và deploy:

- **Cross-compile không còn dễ.** Không thể `GOOS=linux go build` từ Windows nữa. Dùng Docker để build, hoặc build trên chính nền tảng đích.
- **Agent chạy trên máy dev Windows** cần libgphoto2 cho Windows — vốn không sẵn có. Cân nhắc: agent chạy trên Linux/macOS, hoặc dùng phương án Canon CCAPI (thuần HTTP, không cgo) trên Windows.
- **`CGO_ENABLED=0` cho các binary không cần imaging** để giữ image Docker nhỏ và deploy đơn giản.
- **Rò bộ nhớ.** libvips và libraw cấp phát ngoài heap của Go, nên GC của Go không thấy. `runtime.SetFinalizer` là không đủ tin cậy — giải phóng tường minh bằng `defer`, và theo dõi RSS của process trong production.
- **Panic trong C không bắt được.** Một file RAW hỏng có thể làm sập cả process. Chạy việc decode RAW trong worker process riêng biệt, để một file hỏng không kéo theo cả hàng đợi.

Điểm cuối cùng đáng làm ngay từ đầu: worker render nên là process riêng, có thể chết và được restart, không nằm chung với API server.
