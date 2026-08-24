# ADR 0003 — Phương án thay thế nếu không dùng được CascableCore

- **Ngày:** 2026-08-24
- **Trạng thái:** Đã chấp nhận
- **Thay đổi:** phần "Rủi ro mở" và "Xét lại khi nào" của [ADR 0001](0001-capture-strategy.md)

## Bối cảnh

ADR 0001 kết luận CascableCore là đường khả dĩ **duy nhất**, và ghi rủi ro số một
là "Cascable có quyền từ chối cấp license". Phần "xét lại khi nào" nói rằng nếu
họ từ chối thì phải nới ràng buộc, và ràng buộc rẻ nhất để nới là "không laptop".

Khảo sát lại (2026-08) cho thấy kết luận đó **quá bi quan**. Có một đường thứ hai
đã được chứng minh là chạy được.

## Phát hiện

### ImageCaptureCore vẫn hỏng — xác nhận lại

Không có gì thay đổi theo hướng tốt. Trên iOS 26.1 beta,
`requestControlAuthorization()` và `requestContentsAuthorization()` luôn trả
`.notDetermined` và không bao giờ chuyển sang `.authorized` hay `.denied`. Lỗi
kéo dài liên tục từ iOS 18 tới iOS 26.1. Coi như đường này đóng.

### DMA không giúp gì

iOS 26.3 mở phụ kiện cho bên thứ ba theo yêu cầu của DMA, nhưng phạm vi là
**ghép nối Bluetooth và chuyển tiếp thông báo** — AirPods-like pairing. Không
liên quan tới USB host hay PTP. Đừng trông chờ.

### Đã có người làm được đúng thứ ta cần, một mình

**"THE Tether"** ([App Store](https://apps.apple.com/us/app/the-tether/id6768165095))
— app tether Nikon không dây cho iPhone/iPad, do một nhiếp ảnh gia thời trang
người Hàn Quốc tự viết, phát hành 06/2026.

Đã test với: **Z9, Z8, Zf, Z6III, Z7II, Z6II, Z7, Z6, Z5II, Z5, Z50II, Z50,
Zfc, D780** — gần như trùng khớp danh sách body mục tiêu của dự án này.

Kết nối qua router 5GHz hoặc WiFi phát trực tiếp từ máy ảnh. Và kiến trúc họ
mô tả — "xem JPG nhanh khi đang chụp, giữ nguyên quy trình RAW cho hậu kỳ" —
chính là kiến trúc "để ảnh trên thẻ, chỉ kéo preview" của ADR 0001.

**Suy luận, không phải sự thật đã kiểm chứng:** app chỉ hỗ trợ Nikon và chỉ qua
WiFi. Nếu họ dùng CascableCore thì Canon và Sony gần như miễn phí đi kèm cùng một
API. Chỉ-Nikon-chỉ-WiFi là dấu hiệu mạnh cho thấy họ tự implement PTP/IP. Không
xác nhận được điều này từ bên ngoài.

Dù họ dùng cách nào, kết luận vẫn đứng: **một người làm được, và App Store duyệt.**

## Vì sao PTP/IP qua WiFi không vướng giới hạn của Apple

Điểm mấu chốt hay bị bỏ sót khi đọc ADR 0001: mọi rào cản của Apple đều nằm ở
**USB host**. PTP/IP là **TCP socket thuần** — không có framework nào của Apple
đứng giữa, không cần MFi, không cần entitlement.

Nghĩa là ràng buộc "iOS bắt buộc" không hề chặn đường tự implement. Nó chỉ chặn
đường USB.

## Đánh đổi thật giữa hai đường

| | CascableCore | Tự implement PTP/IP |
|---|---|---|
| USB có dây | ✅ | ❌ chỉ WiFi |
| Số hãng máy | 8 hãng, 250+ model | Chỉ Nikon (tự mở rộng) |
| Live view | ✅ | Tự làm |
| Chi phí | License thương lượng | Thời gian kỹ sư |
| Phê duyệt | **Họ có quyền từ chối** | Không cần ai duyệt |
| Bảo trì khi Nikon đổi firmware | Cascable lo | Ta lo |
| Rủi ro giao thức | Không | **Có thật** — libgphoto2 vẫn còn issue mở với Z8 |

### Mất USB có nghiêm trọng không

Ít hơn tưởng tượng, với đúng sản phẩm này. Giá trị cốt lõi là **cho khách xem ảnh
đã lên màu ngay tại buổi chụp**, và luồng đó chỉ cần JPEG preview — thứ mà WiFi
tải thừa sức. RAW vốn đã được thiết kế để **ở lại trên thẻ**.

Chính THE Tether bán điểm này như một ưu điểm: dây cáp vướng víu khi chụp ngoài
trời và trong studio.

Rủi ro thật của WiFi không phải băng thông mà là **môi trường**: tiệc cưới đông
người, nhiễu 2.4GHz nặng, khách sạn có WiFi kém. Router 5GHz riêng trong túi máy
là cách xử lý, và nó không phải "thiết bị trung gian" theo nghĩa ADR 0001 cấm —
không ai phải thao tác trên nó.

## Quyết định

**Vẫn thử CascableCore trước.** Trial miễn phí 30 ngày và nó trả lời được câu hỏi
giá — thông tin cần có để quyết định.

**Nhưng không còn bị dồn vào chân tường.** Nếu Cascable từ chối hoặc báo giá vượt
ngân sách, phương án là **tự implement PTP/IP qua WiFi cho Nikon Z**, không phải
nới ràng buộc "không laptop" như ADR 0001 đã viết.

Điều này cũng có nghĩa: khi thương lượng giá, ta có phương án thay thế thật. Đó là
vị thế khác hẳn.

## Nếu tự implement: những gì cần biết trước

**Cạm bẫy giấy phép.** `libgphoto2` là LGPL và driver ptp2 của nó là nguồn tham
khảo đầy đủ nhất về vendor extension của Nikon. **Đọc để hiểu giao thức thì được;
chép code vào app đóng thì không.** Phải tự viết từ đặc tả và từ phân tích gói tin.

Các nguồn tham khảo (đọc, không chép):

| Dự án | Ghi chú |
|---|---|
| `shezi/airmtp` | Reverse-engineer giao diện WiFi của Nikon. Sát nhất với việc cần làm. |
| `mmattes/ptpip` | PTP/IP bằng Python, dễ đọc để hiểu bắt tay ban đầu |
| `dakmh/camlib` | C99, Apache 2.0, có PTP/IP — **nhưng chưa hỗ trợ Nikon** |
| `gphoto/libgphoto2` | Đầy đủ nhất về Nikon. **LGPL — chỉ đọc.** |

**Rủi ro đã biết:** libgphoto2 vẫn còn issue mở với PTP/IP của Z8 (`PTP error
2005` khi query device property). Tự implement không tự động tránh được lỗi này —
nó nằm ở phía giao thức của Nikon, không phải ở phía thư viện.

**Cách giảm rủi ro:** làm spike 1–2 tuần chỉ để bắt tay PTP/IP và nhận một sự kiện
ảnh mới từ một body thật. Nếu bước đó chạy, phần còn lại là công việc đều đặn. Nếu
không, ta biết sớm với chi phí rẻ.

## Thứ tự ưu tiên khi Cascable không dùng được

1. **Tự implement PTP/IP qua WiFi cho Nikon Z** — đã chứng minh khả thi, không phụ
   thuộc ai, không mất USB nào mà sản phẩm này thật sự cần
2. **Nới "không laptop"** — hot-folder qua NX Tether. Rẻ và ổn định nhất, nhưng
   thêm ma sát vận hành
3. **Android trước** — libgphoto2 NDK + USB Host. Bỏ iOS-first
4. **SnapBridge** — 2MP tự động, 8MP trên Z đời mới, **không có RAW**. Chỉ đủ nếu
   định vị lại sản phẩm thành "xem nhanh tại chỗ"

## Ghi chú cạnh tranh

THE Tether không chỉ là bằng chứng kỹ thuật, nó còn là **đối thủ**. Nó làm phần
tether và review; nó **không** làm màu riêng của người dùng và không làm AI. Đó là
chỗ sản phẩm này khác biệt — nhưng đừng giả định thị trường còn trống.

Evoto cũng có tài liệu hướng dẫn tether không dây với Nikon trên iPad, nên hướng
này không còn mới với người dùng.
