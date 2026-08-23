# Quy ước làm việc

## Nhánh

Dự án dùng Git Flow rút gọn. Hai nhánh sống mãi, phần còn lại là nhánh tạm.

| Nhánh | Vai trò | Xoá sau khi merge |
|---|---|---|
| `main` | Luôn deployable. Chỉ nhận merge từ `release/*` và `hotfix/*`. | Không |
| `develop` | Nhánh tích hợp. Mọi công việc thường ngày đổ về đây. | Không |
| `feature/<slug>` | Tính năng mới. Tách từ `develop`. | Có |
| `fix/<slug>` | Sửa lỗi chưa lên production. Tách từ `develop`. | Có |
| `docs/<slug>` | Tài liệu, ADR. Tách từ `develop`. | Có |
| `chore/<slug>` | Hạ tầng, config, dependency. Tách từ `develop`. | Có |
| `release/<version>` | Đóng băng để chuẩn bị phát hành. Tách từ `develop`. | Có |
| `hotfix/<slug>` | Sửa gấp trên production. **Tách từ `main`.** | Có |

`<slug>` viết thường, phân cách bằng gạch ngang, mô tả kết quả chứ không mô tả
hành động: `feature/capture-contract`, không phải `feature/add-capture-stuff`.

Nếu có mã issue, đặt trước slug: `feature/42-capture-contract`.

### Vì sao merge bằng `--no-ff`

Mọi nhánh merge vào `develop` và `main` đều dùng `--no-ff`:

```bash
git switch develop
git merge --no-ff feature/capture-contract
git branch -d feature/capture-contract
```

Fast-forward làm biến mất ranh giới của tính năng trong lịch sử. Sáu tháng nữa,
khi cần biết "những commit nào thuộc về việc thêm hợp đồng capture", merge commit
là thứ duy nhất trả lời được. Nó cũng khiến `git revert -m 1` gỡ được cả tính năng
bằng một lệnh.

### Ba nhánh đầu tiên nằm ngoài quy ước này

`chore/repo-setup`, `docs/capture-research`, `docs/adr-0001-capture-strategy` được
merge thẳng vào `main` trước khi `develop` tồn tại. Cố ý không viết lại lịch sử để
làm đẹp — repo chưa deploy, chưa có release, việc rewrite chỉ tạo rủi ro mà không
đem lại gì. Từ `develop` trở đi, quy ước trên được áp dụng đủ.

## Commit

Theo [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <mô tả ngắn, viết thường, không dấu chấm cuối>

<thân commit: giải thích VÌ SAO, không phải LÀM GÌ — diff đã nói làm gì>
```

Type dùng trong dự án: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `perf`, `build`, `ci`.

Thân commit là chỗ quan trọng nhất. Diff cho biết code thay đổi thế nào; chỉ có
thân commit cho biết vì sao đã chọn cách đó thay vì cách khác. Với dự án này —
nơi phần lớn quyết định là kết quả của việc loại trừ các phương án bất khả thi —
thiếu phần "vì sao" khiến người sau vô tình đi lại đường đã cụt.

## Quy tắc riêng của dự án

Ba luật dưới đây bắt nguồn từ [ADR 0001](docs/adr/0001-capture-strategy.md).
Vi phạm chúng khiến việc thêm Android trở thành viết lại app.

1. **Rẽ nhánh theo `capabilities`, không theo hãng máy hay SDK.** libgphoto2 không
   có live view với Nikon; CascableCore thì có. Cùng body, hai đường capture, hai
   tập khả năng.
2. **Pixel không đi qua cầu JS.** Ảnh tham chiếu bằng `ImageHandle` (URI native).
   Một NEF là 50–60MB.
3. **Chữ "Cascable" không xuất hiện ngoài native module iOS.** Thấy nó ở tầng trên
   là rò rỉ trừu tượng.

## Không được commit

- **File RAW** (`.NEF`, `.CR3`, `.ARW`...) — mỗi file 50–60MB, và có thể là ảnh khách hàng
- **Binary hoặc file license của CascableCore** — SDK thương mại
- **Model weights** (`.onnx`, `.pt`, `.safetensors`) — dùng Git LFS hoặc object storage
- **Secret** — `.env`, `.pem`, `.p12`, `.mobileprovision`

`.gitignore` đã chặn sẵn, nhưng `git add -f` thì vượt qua được. Đừng.

## Trước khi mở pull request

- [ ] Nhánh tách từ `develop` (trừ `hotfix/*` tách từ `main`)
- [ ] Commit theo Conventional Commits, thân commit giải thích vì sao
- [ ] Không có secret, file RAW, hay binary SDK trong diff
- [ ] Nếu thay đổi một quyết định kiến trúc, ADR tương ứng đã được cập nhật
