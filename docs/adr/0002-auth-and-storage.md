# ADR 0002 — Xác thực và chiến lược lưu trữ

- **Ngày:** 2026-08-24
- **Trạng thái:** Đã chấp nhận
- **Liên quan:** [ADR 0001](0001-capture-strategy.md)

## Bối cảnh

Cần bốn tính năng: đăng ký/đăng nhập, đăng nhập Google và Apple, mua dung lượng,
và liên kết Drive để người dùng tự chọn nơi lưu trữ.

Nghe như bốn việc độc lập. Thực tế chúng bị ràng buộc chặt vào nhau bởi ba quy
định bên ngoài mà nếu phát hiện muộn sẽ phải làm lại kiến trúc.

## Ba ràng buộc bên ngoài

### 1. Có đăng nhập Google thì BẮT BUỘC có Sign in with Apple

App Store Guideline 4.8: app dùng dịch vụ đăng nhập bên thứ ba (Google, Facebook,
LinkedIn...) làm phương thức chính **phải** đồng thời cung cấp Sign in with Apple
như một lựa chọn tương đương.

Miễn trừ duy nhất áp dụng được: app **chỉ** dùng hệ thống tài khoản của chính
công ty. Vì yêu cầu đã bao gồm Google, miễn trừ này không dùng được.

Lựa chọn tương đương còn phải: chỉ thu thập tên và email, cho phép người dùng ẩn
email thật, và không thu thập hành vi trong app cho mục đích quảng cáo nếu chưa
có đồng ý.

Vì vậy Sign in with Apple **không phải tính năng tuỳ chọn** — nó là điều kiện để
được lên App Store.

### 2. Bán dung lượng do mình quản lý = dịch vụ số → phải qua Apple IAP

Đây là ràng buộc tốn tiền nhất và dễ bị bỏ qua nhất.

| Storefront | Bán dung lượng của mình |
|---|---|
| **Hoa Kỳ** | Được phép dẫn link thanh toán ngoài, **không mất hoa hồng**, không cần entitlement — hệ quả của phán quyết Epic v. Apple năm 2025 |
| **Việt Nam và phần còn lại** | **Bắt buộc IAP**, Apple thu 15–30% |

Nếu thị trường chính là Việt Nam thì mỗi đồng doanh thu dung lượng mất 15–30% cho
Apple. Đó là một khoản thật, phải đưa vào mô hình giá ngay từ đầu chứ không phải
phát hiện sau khi đã đặt giá.

Guideline 3.1.2(c) còn buộc phải ghi rõ người dùng nhận được bao nhiêu dung lượng
với mức giá đó.

**Liên kết Drive là lời giải cấu trúc cho vấn đề này**, không phải một mẹo lách:
khi người dùng liên kết Drive của chính họ, ta không bán dung lượng nào cả. Họ mua
dung lượng từ Google, ta chỉ bán chức năng của app. Không có giao dịch số nào để
Apple thu hoa hồng.

### 3. Phạm vi Google Drive quyết định có phải qua kiểm định bảo mật hay không

| Scope | Hệ quả |
|---|---|
| `drive.file` — chỉ file do app tạo | **Không** cần restricted-scope verification, **không** cần CASA |
| `drive.readonly`, `drive` đầy đủ | Restricted scope → **CASA audit**, tốn tiền, phải kiểm định lại **mỗi 12 tháng** |

Google khuyến nghị thẳng: dùng `drive.file` kết hợp Google Picker để người dùng
tự chọn file muốn cho app truy cập.

Đây là **ràng buộc kiến trúc cứng**. Nếu sau này có ai đề xuất "cho người dùng
duyệt thư mục Drive sẵn có của họ", đó không phải một tính năng nhỏ — nó đẩy dự án
vào diện CASA với chi phí và chu kỳ kiểm định hàng năm.

## Quyết định

### Xác thực

Ba phương thức, ngang hàng nhau:

1. **Sign in with Apple** — bắt buộc theo 4.8
2. **Google Sign-In**
3. **Email + mật khẩu** — cho người dùng không muốn dùng cả hai

**Token luôn được xác minh phía server.** Client gửi ID token; server tự lấy JWKS
của Google/Apple, kiểm chữ ký, `iss`, `aud`, `exp`, và `nonce`. Không bao giờ tin
danh tính do client tự khai — một client bị sửa đổi có thể khai là bất kỳ ai.

**Nonce là bắt buộc, không phải tuỳ chọn.** Không có nonce thì một ID token hợp lệ
bị chặn được có thể phát lại để đăng nhập.

**Một người, nhiều danh tính.** Cùng một email có thể đến từ Apple, Google và
mật khẩu. Mô hình là `users` (một người) và `identities` (nhiều cách đăng nhập),
không phải nhét provider vào bảng users.

Cạm bẫy riêng của Apple: **Apple chỉ trả tên và email ở lần uỷ quyền ĐẦU TIÊN.**
Các lần sau chỉ có `sub`. Không lưu ngay ở lần đầu là mất vĩnh viễn, và không có
cách lấy lại. Đây là lỗi rất hay gặp.

Cạm bẫy riêng của email ẩn: Apple cấp địa chỉ relay `@privaterelay.appleid.com`.
Không được coi email là khoá định danh chính — `sub` mới là khoá ổn định.

### Lưu trữ: nhà cung cấp cắm được, người dùng chọn

Bốn lựa chọn, cùng một interface:

| Provider | Ai trả tiền dung lượng | Server có bytes không |
|---|---|---|
| `device` | Không ai — ảnh ở trên thẻ/máy | Không |
| `managed` | Người dùng mua của ta | Có |
| `google_drive` | Người dùng trả Google | Chỉ khi được cấp quyền |
| `icloud` | Người dùng trả Apple | Không |

Điều này khớp thẳng với kiến trúc "để ảnh trên thẻ" của ADR 0001: một buổi chụp là
hơn 100GB, và việc mặc định nhét toàn bộ vào hạ tầng của mình vừa đắt vừa không
cần thiết.

**Hệ quả nghiêm trọng phải nói rõ với người dùng trong sản phẩm:** với
`google_drive` và `icloud`, dữ liệu nằm ngoài tầm kiểm soát của app. Người dùng
hết dung lượng, thu hồi quyền, hoặc xoá file trong Drive thì ảnh biến mất khỏi app
và ta không làm gì được. Điều này phải được nói rõ **lúc họ chọn**, không phải
trong điều khoản sử dụng.

**Render RAW phía server chỉ khả dụng khi server đọc được bytes.** Với `drive.file`,
app đọc được file do chính nó tạo, nên `google_drive` vẫn render được **nếu** giữ
refresh token. Với `icloud` và `device` thì không — bản xuất chất lượng cao phải
render trên thiết bị hoặc không có. Đây là khác biệt về tính năng giữa các lựa
chọn, phải hiển thị cho người dùng thấy khi chọn.

### Quota

Chỉ áp dụng cho `managed`. Với các provider khác, quota là chuyện giữa người dùng
và Google/Apple; app chỉ đọc và hiển thị, không cưỡng chế.

## Hệ quả

- Bảng `image_assets` cần thêm cột `provider`: cùng một ảnh có thể có preview ở
  `managed` và bản gốc ở `google_drive`.
- Người dùng đổi provider **không** tự động di chuyển dữ liệu cũ. Di chuyển là
  thao tác tường minh, tốn thời gian, và cần thông báo tiến độ.
- Tính giá phải trừ 15–30% hoa hồng Apple cho mọi storefront ngoài Hoa Kỳ.
- Cần một điểm gọi duy nhất cho IAP hai nền tảng. RevenueCat là lựa chọn phổ biến;
  chưa chốt, và phải cân nhắc thêm chi phí của chính nó.

## Rủi ro mở

1. **Chưa có thẩm định pháp lý.** Nội dung ở đây đọc từ tài liệu công khai của
   Apple và Google, không phải tư vấn pháp lý. Trước khi phát hành có thu tiền,
   cần luật sư rà soát — đặc biệt phần thuế và điều khoản người dùng.
2. **Quy định App Store thay đổi nhanh.** Miễn trừ cho storefront Hoa Kỳ mới có từ
   2025 và đang trong tranh chấp pháp lý. Không xây mô hình doanh thu chỉ dựa vào
   nó tồn tại mãi.
3. **`drive.file` không cho đọc file người dùng tạo bằng tay.** Nếu khách kỳ vọng
   "app thấy được thư mục ảnh có sẵn của tôi", kỳ vọng đó không thể đáp ứng mà
   không bước vào diện CASA. Cần nói rõ trong sản phẩm.

## Xét lại quyết định này khi nào

- Apple bỏ hoặc mở rộng miễn trừ thanh toán ngoài → tính lại mô hình giá
- Khách hàng thật sự cần duyệt Drive sẵn có → phải cân nhắc chi phí CASA một cách
  nghiêm túc, đừng lặng lẽ đổi scope
- Nếu `managed` chiếm phần lớn người dùng, chi phí lưu trữ sẽ thành khoản vận hành
  lớn nhất — khi đó cân nhắc đẩy mạnh liên kết Drive
