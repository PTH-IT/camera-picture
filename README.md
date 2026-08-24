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
mobile/src/color/       Áp LUT trên GPU
  haldLut.ts            Shader SkSL + factory runtime effect
  LutImage.tsx          Component hiển thị ảnh đã áp LUT
mobile/src/account/     Hợp đồng xác thực + lựa chọn lưu trữ
backend/
  cmd/api/              HTTP server
  cmd/lutconv/          .cube -> hald PNG (nguồn sinh LUT duy nhất)
  internal/api/         Handler + validation + xác thực + phân quyền
  internal/auth/        Xác minh ID token Apple/Google, mật khẩu, phiên
  internal/secrets/     Mã hoá refresh token khi lưu (AES-GCM)
  internal/storage/     Nhà cung cấp lưu trữ cắm được
    miniostore/         MinIO / S3 — provider `managed`
    gdrive/             Google Drive của người dùng, scope drive.file
  internal/billing/     Mua dung lượng qua IAP, quyền lợi, hạn mức
  internal/store/       Interface lưu trữ; store/memory cho test và chạy thử
  internal/imaging/lut/ Parse .cube, sinh hald, áp LUT phía server
  internal/protocol/    Hợp đồng dữ liệu, phản chiếu của TS
  internal/ids/         UUID v7
  internal/migrate/     Chạy migration lúc khởi động
  internal/store/pg/    Bản Postgres của mọi repo
  internal/store/storetest/  Bộ test tuân thủ dùng chung
  migrations/           Lược đồ Postgres (nhúng vào binary)
docs/adr/               Architecture decision records
docs/hald-lut-format.md Hợp đồng layout LUT giữa thiết bị và server
.claude/skills/photo-tether-app/   Kiến thức miền đã kiểm chứng
```

`.claude/skills/photo-tether-app/` chứa nghiên cứu đã kiểm chứng về Nikon SDK, libgphoto2, CascableCore, pipeline màu, và model AI. Claude Code tự đọc khi cần; con người cũng đọc được.

## Pipeline màu

Đã implement và kiểm chứng. Sinh LUT cho app:

```bash
cd backend && go run ./cmd/lutconv -v -out ../mobile/assets/luts ../luts/*.cube
```

Luôn dùng lệnh này, đừng chuyển đổi bằng công cụ khác — layout có thể khác và màu
sẽ lệch mà không có thông báo lỗi nào.

Test đối chiếu thiết bị/server (`go test ./internal/imaging/lut/ -v`) đo lệch lớn
nhất **0,58 mức trên thang 8-bit** qua các LUT 17³/32³/33³/64³/65³ — dưới ngưỡng
nhìn thấy được. Quy ước layout và lý do từng dòng công thức:
[docs/hald-lut-format.md](docs/hald-lut-format.md).

## Đồng bộ

Giao thức được tối ưu cho đúng một việc: **đẩy hàng nghìn bản ghi metadata rẻ và
idempotent**, vì phần lớn ảnh không bao giờ lên server.

```
POST   /v1/sessions
POST   /v1/sessions/{id}/images/batch    đẩy metadata, idempotent theo clientId
GET    /v1/sessions/{id}/changes?since=  kéo delta bằng con trỏ revision
PUT    /v1/images/{id}/edit              chỉnh sửa không phá huỷ
POST   /v1/images/{id}/assets/confirm    báo upload xong
DELETE /v1/images/{id}                   xoá mềm
```

Hai quyết định đáng chú ý:

- **Con trỏ đồng bộ là số nguyên logic do server cấp, không phải timestamp.** Đồng
  hồ máy ảnh, điện thoại và server đều lệch nhau, và hai thay đổi trong cùng một
  mili giây sẽ khiến con trỏ kiểu timestamp bỏ sót bản ghi một cách âm thầm.
- **Mỗi bản ghi thay đổi mang một revision riêng biệt.** Nếu dùng chung revision
  cho cả lô, phân trang sẽ bỏ sót: client lấy nửa nhóm, đặt con trỏ bằng revision
  đó, nửa còn lại vĩnh viễn không thoả `> since`. Ảnh biến mất mà không có lỗi nào.
  `TestDeltaSyncLosesNothing` ép buộc điều này.

**File không đi qua Go API.** Client upload thẳng lên object storage bằng presigned
URL rồi gọi `assets/confirm`. Cho một NEF 60MB chảy qua handler sẽ giữ goroutine và
băng thông suốt thời gian upload.

## Xác thực và lưu trữ

Ba phương thức đăng nhập ngang hàng: **Sign in with Apple, Google, và email + mật
khẩu**. Apple không phải tính năng tuỳ chọn — App Store guideline 4.8 bắt buộc có
nó khi đã có Google.

```
POST /v1/auth/signup   /signin   /oidc   /signout   /signout-everywhere
GET  /v1/me
```

Bốn quyết định đáng biết:

- **ID token luôn xác minh phía server** bằng JWKS của nhà cung cấp. Client sửa
  được thì khai được là bất kỳ ai.
- **Nonce bắt buộc.** Không có nó, một ID token hợp lệ bị chặn được có thể phát
  lại để đăng nhập dưới danh nghĩa nạn nhân.
- **Chỉ tự động ghép tài khoản khi CẢ HAI phía có email đã xác minh.** Nếu không,
  kẻ tấn công đăng ký tài khoản mật khẩu bằng email nạn nhân rồi chờ nạn nhân
  đăng nhập Google là chiếm được tài khoản.
- **Token phiên dạng mờ, không phải JWT.** JWT không thu hồi được, nên "đăng xuất
  khỏi mọi thiết bị" sau khi mất máy sẽ không có tác dụng thật.

Người dùng chọn nơi lưu ảnh: `device`, `managed`, `google_drive`, `icloud`. Mỗi
lựa chọn khai báo **khả năng** của nó, và UI rẽ nhánh theo khả năng chứ không
theo tên — `icloud` chẳng hạn không cho kết xuất RAW phía máy chủ, và điều đó
phải hiện ra trước khi người dùng chọn.

Google Drive dùng scope **`drive.file`** và đây là ràng buộc cứng: scope rộng hơn
kéo theo kiểm định CASA tốn tiền và phải làm lại mỗi 12 tháng.

### Hai provider đã cài

**MinIO / S3 (`managed`)** — client tải lên bằng presigned URL, file không đi qua
Go API. Một điều đã kiểm chứng bằng test với MinIO thật: **presigned PUT không
ràng buộc kích thước.** Client khai 100 byte rồi tải lên 2MB thì S3 vẫn nhận. Nên
kiểm hạn mức lúc cấp URL chỉ là tư vấn; cưỡng chế thật nằm ở `Confirm`, nơi server
hỏi kích thước thật rồi xoá file nếu vượt.

**Google Drive (`google_drive`)** — client tải thẳng lên Google qua phiên
resumable, server không chạm vào bytes. Refresh token được **mã hoá AES-GCM và
buộc vào ngữ cảnh** (userID + provider), nên bản mã bị chép sang dòng khác trong
cơ sở dữ liệu là vô dụng.

```bash
docker compose up -d          # minio + postgres
cd backend && go test ./...   # gồm cả test tích hợp
```

Migration chạy tự động lúc khởi động, có advisory lock nên rolling deploy nhiều
bản sao cùng lúc vẫn an toàn.

### Biến môi trường

| | |
|---|---|
| `GOOGLE_CLIENT_IDS`, `APPLE_CLIENT_IDS` | Xác minh ID token, phân tách bằng dấu phẩy |
| `STORAGE_SECRET_KEY` | Khoá base64 32 byte để mã hoá refresh token |
| `DATABASE_URL` | Postgres. **Thiếu thì chạy trong bộ nhớ và mất sạch dữ liệu khi tắt** |
| `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`, `MINIO_BUCKET` | Provider `managed` |
| `GOOGLE_DRIVE_CLIENT_ID`, `GOOGLE_DRIVE_CLIENT_SECRET`, `GOOGLE_DRIVE_REDIRECT_URI` | Provider Drive |

Thiếu nhóm nào thì endpoint tương ứng trả **501** kèm mã `not_configured`, chứ
không sập lúc khởi động — client ẩn nút thay vì báo lỗi.

## Trạng thái

| Phần | Trạng thái |
|---|---|
| Pipeline màu | Chạy được, có test đối chiếu thiết bị/server |
| API đồng bộ | Chạy được, có test; **store mới là bản in-memory** |
| Hợp đồng capture | Scaffold — **chưa tether được** |
| Lược đồ Postgres | ✅ Đã chạy và test với Postgres thật |
| Xác thực | **Chưa có** — userID đang hardcode |

Tầng capture chưa làm là chủ ý: 5 việc dưới đây phải xong trước, vì kết quả có thể
thay đổi kiến trúc.

### Việc phải làm, theo thứ tự

1. Chuẩn bị body và thiết bị test **trước**, rồi mới kích hoạt **trial 30 ngày CascableCore** (`developer.cascable.se`) — đồng hồ chạy ngay khi kích hoạt
2. **Song song:** liên hệ Cascable về giá và partnership criteria — **họ có quyền từ chối**, đây là rủi ro dự án số một
3. Test đúng danh sách body, đặc biệt Z6III / Z5II / Z50II / Zf / ZR — bảng compatibility công khai mới chỉ xác nhận cho Cascable Studio, chưa cho SDK
4. Đo thời gian từ lúc bấm máy đến lúc preview hiện trên điện thoại, qua USB-C và qua WiFi
5. Kiểm chứng `previewWithoutFullDownload` — nếu CascableCore bắt buộc tải cả file RAW mới đọc được preview nhúng, **toàn bộ chiến lược lưu trữ phải thiết kế lại**

## Yêu cầu môi trường

Go 1.23+ (đã kiểm thử với 1.26.7), Node 20+. Phía mobile cần
`@shopify/react-native-skia` và React Native New Architecture (Turbo Modules).

```bash
cd backend && go test ./...
```

## Giấy phép

Proprietary — Copyright (c) 2026 PTH-IT. All rights reserved. Xem [LICENSE](LICENSE).

Giấy phép của **thư viện bên thứ ba không bị thay thế** bởi thông báo này. CascableCore cần license thương mại riêng; model AI có bản hạn chế thương mại. Phải rà soát trước khi phát hành — xem [ADR 0001](docs/adr/0001-capture-strategy.md) và `ai-features.md` trong skill.
