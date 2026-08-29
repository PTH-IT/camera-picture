# Chạy dự án trên một máy khác

## Điều quan trọng nhất

**Gần như mọi thứ cần thiết đã nằm trong repo, và đó là có chủ ý.**

Các quyết định kiến trúc không nằm trong đầu ai hay trong lịch sử chat — chúng
nằm ở `docs/adr/`, `docs/hald-lut-format.md`, `CONTRIBUTING.md`, và trong chú
thích ngay cạnh đoạn code chúng chi phối. Thư mục `.claude/skills/photo-tether-app/`
cũng được commit, nên một phiên Claude Code mới trên máy khác đọc được toàn bộ bối
cảnh đã kiểm chứng về Nikon, CascableCore, pipeline màu và các phương án đã bị loại.

Cụ thể, những thứ này đi theo repo:

- Toàn bộ mã nguồn backend, mobile, preview
- ADR 0001–0003: vì sao chọn CascableCore, ràng buộc App Store, phương án thay thế
- `CONTRIBUTING.md`: quy ước nhánh và commit
- `docker-compose.yml`, cấu hình CI
- `.env.example`: 17 biến môi trường kèm giải thích từng cái
- Skill với nghiên cứu đã kiểm chứng

## Cần cài sẵn

| | Ghi chú |
|---|---|
| Go 1.23+ | Đã kiểm thử với 1.26.7 |
| Node 20+ | |
| Docker | Cho Postgres và MinIO |
| Chrome | Chỉ để chụp màn hình bản xem trước |
| macOS + Xcode | **Chỉ khi** build iOS |

## Chạy từ đầu

```bash
git clone https://github.com/PTH-IT/camera-picture.git
cd camera-picture

cp .env.example .env
openssl rand -base64 32          # dán vào STORAGE_SECRET_KEY

docker compose up -d             # postgres + minio
cd backend && go run ./cmd/api   # migration tự chạy lúc khởi động
```

Kiểm tra:

```bash
cd backend && go test ./...      # gồm cả test tích hợp với Postgres và MinIO thật
cd mobile  && npm install && npx tsc --noEmit
cd preview && npm install && npm run dev    # xem giao diện
```

Server đọc `.env` ở thư mục hiện tại hoặc thư mục cha. **Biến môi trường đã đặt
sẵn luôn thắng file** — trên production và CI, cấu hình thật không bao giờ bị một
file `.env` lỡ tay commit đè lên.

## Những thứ KHÔNG đi theo repo

### 1. Bí mật

`.env` bị `.gitignore` chặn, và đó là đúng. Trên máy mới phải điền lại.

**`STORAGE_SECRET_KEY` là trường hợp đặc biệt và cần cẩn thận.** Nó mã hoá refresh
token của Google Drive. Nếu bạn mang theo cơ sở dữ liệu cũ mà đổi khoá, mọi token
đã lưu không giải mã được nữa và toàn bộ người dùng đã liên kết Drive phải liên kết
lại — họ sẽ không hiểu vì sao. Bắt đầu với dữ liệu trống thì sinh khoá mới thoải mái.

### 2. Dữ liệu Postgres và MinIO

Nằm trong Docker volume trên máy cũ. Với dữ liệu phát triển thì cứ bỏ — migration
dựng lại lược đồ trống trong vài giây. Nếu thật sự cần mang theo:

```bash
# máy cũ
docker exec camera-postgres-1 pg_dump -U camera camera > dump.sql
# máy mới
docker exec -i camera-postgres-1 psql -U camera camera < dump.sql
```

Nhớ mang theo đúng `STORAGE_SECRET_KEY` cùng với dump, nếu không phần refresh token
trong đó thành rác.

### 3. Bộ nhớ của Claude Code

Nằm ở `~/.claude/projects/<tên-thư-mục-dự-án>/memory/`, **không** trong repo. Đây là
ghi chú riêng của Claude về dự án. Chép cả thư mục `memory/` sang máy mới nếu muốn
giữ.

Không bắt buộc: mọi thứ trong đó chỉ trỏ ngược về các ADR trong repo.

### 4. Lịch sử phiên làm việc

Đây là thứ **không** chuyển được một cách chính thức. Phiên Claude Code lưu cục bộ
theo máy.

Có một cách không chính thức: bản ghi phiên nằm ở

```
~/.claude/projects/<đường-dẫn-dự-án-đã-mã-hoá>/<session-id>.jsonl
```

Tên thư mục là đường dẫn dự án với dấu phân cách thay bằng `-`. Trên máy này,
`G:\camera` thành `G--camera`. Chép file `.jsonl` sang thư mục tương ứng với đường
dẫn MỚI trên máy mới rồi chạy `claude --resume` thì phiên có thể xuất hiện trong
danh sách.

Hai lưu ý thật thà:

- Đây là bố cục nội bộ, **không có tài liệu chính thức** và có thể đổi giữa các
  phiên bản. Đừng xây quy trình làm việc dựa vào nó.
- Bản ghi phiên này nặng **7,7 MB**. Nạp lại toàn bộ vào ngữ cảnh vừa chậm vừa
  tốn, và phần lớn nội dung là các bước đã hoàn tất và không còn liên quan.

**Cách tốt hơn:** bắt đầu phiên mới trên máy kia và nói với Claude đọc
`README.md`, `docs/adr/`, và skill trong `.claude/skills/`. Toàn bộ quyết định đều
ở đó, viết ra để đọc, chứ không phải để suy lại từ lịch sử chat. Đó chính là lý do
chúng được viết ra ngay từ đầu.

## Nếu chuyển sang máy macOS

Đây là lúc mở khoá được phần iOS:

1. Dựng dự án native: `npx @react-native-community/cli init` rồi ghép `mobile/src`
   vào, hoặc thêm thư mục `ios/` vào chính `mobile/`
2. Thả `mobile/ios/CaptureSource/` vào dự án Xcode
3. Chạy với `MockBackend` trước, xác nhận toàn bộ app hoạt động
4. Chỉ sau đó mới đổi sang `CascableBackend` và điền các mục `NEEDS SDK`

Tách bước 3 và 4 là có lý do: nếu chạy thẳng với SDK và gặp lỗi, bạn không biết
lỗi nằm ở app hay ở phần tích hợp.

Xem [mobile/ios/CaptureSource/README.md](../mobile/ios/CaptureSource/README.md).

## Khác biệt giữa các hệ điều hành

Ba chỗ trong repo có hành vi riêng trên Windows, đều đã được xử lý:

- **cgo**: backend hiện không dùng cgo nên cross-compile bình thường. Khi thêm
  libraw/libvips cho kết xuất RAW thì điều đó đổi — xem `.claude/skills/`,
  `references/backend-go.md`.
- **Chụp màn hình bản xem trước**: `preview/shoot.mjs` đặt viewport qua CDP chứ
  không dùng cờ `--window-size` của Chrome, vì trên Windows Chrome có chiều rộng
  cửa sổ tối thiểu và cờ đó cho ra ảnh sai kích thước.
- **Kết thúc dòng**: `.gitattributes` chuẩn hoá về LF. Trên macOS/Linux sẽ không
  còn thấy cảnh báo CRLF.
