# Nikon — hồ sơ chi tiết

Dự án này lấy Nikon làm hãng chính. Nikon là trường hợp khó nhất về tether nhưng lại có lợi thế độc nhất về màu. File này ghi lại tình trạng đã kiểm chứng (tra cứu 2026-08) và các quyết định đi kèm.

Mục lục:
- [Bốn sự thật định hình mọi quyết định](#bốn-sự-thật-định-hình-mọi-quyết-định)
- [Đường capture — so sánh](#đường-capture--so-sánh)
- [Nikon SDK](#nikon-sdk)
- [libgphoto2 với Nikon Z](#libgphoto2-với-nikon-z)
- [Hot-folder qua NX Tether](#hot-folder-qua-nx-tether)
- [Lợi thế màu: Picture Control](#lợi-thế-màu-picture-control)
- [Ràng buộc thực tế của dự án này](#ràng-buộc-thực-tế-của-dự-án-này)
- [Phase 0 bắt buộc](#phase-0-bắt-buộc)

## Bốn sự thật định hình mọi quyết định

1. **Nikon SDK miễn phí nhưng chỉ chạy Windows 11 64-bit và macOS.** Không Linux, không ARM, không mobile. Điều này giết chết phương án Raspberry Pi agent nếu dùng SDK chính thức, và có nghĩa render worker dùng NEF SDK không deploy được lên container Linux thông thường.

2. **Live view tuỳ đường capture.** libgphoto2 và các phần mềm kiểu Lightroom không có live view với Nikon — nhưng **CascableCore thì có**. Vì dự án đi CascableCore, live view là dùng được.

3. **Firmware Nikon có tiền sử phá vỡ tether.** Z8 firmware 2.0 từng làm hỏng tethering ngay cả trong Lightroom. Bất kỳ implementation PTP tự viết nào cũng phải chấp nhận rủi ro này lặp lại.

4. **Nikon có Picture Control tùy biến nạp được vào thân máy.** Đây là lợi thế thật, không hãng nào khác cho làm sạch sẽ như vậy — xem mục [Lợi thế màu](#lợi-thế-màu-picture-control).

## Đường capture — so sánh

| Đường | Điều khiển máy | Ổn định | Nền tảng agent | Công sức | Rủi ro firmware |
|---|---|---|---|---|---|
| **Hot-folder + NX Tether** | ❌ Không | ✅ Cao nhất | Win / macOS | Rất thấp | Nikon tự lo |
| **Nikon SDK (cgo)** | ✅ Đầy đủ | ✅ Cao | Win / macOS | Trung bình | Nikon tự lo |
| **libgphoto2 (cgo/CLI)** | ✅ Phần lớn | ⚠️ Trung bình | Win / macOS / **Linux** | Trung bình | **Cao** |
| **PTP/IP WiFi tự viết** | ⚠️ Hạn chế | ❌ Thấp | Bất kỳ | Rất cao | **Rất cao** |

Khuyến nghị mặc định: **hot-folder cho MVP → Nikon SDK cho v2.** Chỉ chọn libgphoto2 nếu agent bắt buộc phải chạy Linux/ARM. Không chọn PTP/IP tự viết trừ khi có lý do sống còn.

## Nikon SDK

Cổng đăng ký: `sdk.nikonimaging.com/apply/`. Hai gói riêng:

- **Camera Remote Control SDK** — điều khiển từ xa, chụp, tải ảnh, đổi setting.
- **NEF/NRW RAW SDK** — decode RAW bằng chính engine của Nikon.

Điều cần biết:
- **Miễn phí**, chỉ cần đăng ký tài khoản và đồng ý license. Không phải NDA nặng như một số hãng khác.
- **Không có hỗ trợ kỹ thuật.** Nikon nói thẳng điều này. Gặp bug là tự xử lý.
- Phủ Z5, Z6/Z6II, Z7/Z7II, Z8, Z9, Z50, và nhiều DSLR dòng D. **Kiểm tra lại danh sách khi bắt đầu** — nó thay đổi theo từng bản SDK, và các body mới (Z6III, Z5II, Z50II, Zf, ZR) cần xác nhận riêng.
- **Đọc kỹ license agreement trước khi thương mại hóa.** Điều khoản nằm trong file tải về, không public trên web. Đây là việc phải làm trước khi viết dòng code đầu tiên, không phải sau.
- Là thư viện C → gọi từ Go qua cgo. Nhưng hệ quả: **binary phải build và chạy trên Windows hoặc macOS.**

Về NEF RAW SDK: hấp dẫn vì cho màu đúng chuẩn Nikon, nhưng ràng buộc Windows/macOS khiến nó không hợp với render worker Linux. Quyết định thực dụng: **dùng libraw trên Linux cho MVP**. Chỉ cân nhắc render node Windows nếu người dùng thật sự phàn nàn về sai lệch màu so với NX Studio.

## libgphoto2 với Nikon Z

Hoạt động được nhưng gồ ghề. Các vấn đề đã ghi nhận:

- **USB**: `--capture-tethered` với Z6 II lỗi "PTP Device Prop Not Supported" vì thiếu property `501c` (Focus Metering Mode) trong bảng hỗ trợ.
- **PTP/IP (WiFi)**: kết nối được nhưng không query được device properties — Z8 trả "PTP error 2005". Còn bug làm máy treo sau khi truyền host→camera.
- **Live view**: không dùng được với Nikon.

Kết luận: USB ổn hơn PTP/IP rất nhiều. Nếu buộc phải dùng libgphoto2, đi USB và giới hạn tập lệnh ở mức tối thiểu (nhận sự kiện ảnh mới + tải file), đừng phụ thuộc vào việc đọc/ghi setting.

## Hot-folder qua NX Tether

Đây là đường ít rủi ro nhất để có MVP chạy được, và thường bị bỏ qua vì trông "không đủ kỹ thuật".

**NX Tether** là phần mềm tether chính thức, miễn phí của Nikon, hỗ trợ cả có dây lẫn không dây, phủ Z8/Z9 và các body hiện hành (bản 2.5.0, cập nhật 2026-03). Cần đăng nhập tài khoản Nikon.

Kiến trúc:
```
Nikon body ──USB──▶ NX Tether (Win/Mac) ──ghi file──▶ thư mục
                                                      │ fsnotify
                                          Go agent ◀──┘
                                              │ trích JPEG preview nhúng
                                              ▼ WebSocket/LAN
                                          RN app
```

Vì sao đáng chọn cho MVP:
- **Không viết một dòng PTP nào.** Toàn bộ nỗi đau protocol do Nikon gánh.
- **Miễn nhiễm với firmware update** — Nikon có động cơ giữ NX Tether hoạt động.
- Go agent chỉ cần `fsnotify` + trích preview + đẩy lên. Vài trăm dòng code.
- Cho phép tập trung toàn bộ effort phase 1 vào phần tạo giá trị thật: màu và AI.

Đánh đổi phải nói rõ với người dùng:
- **Không điều khiển được máy ảnh từ app** (không bấm chụp từ xa, không đổi setting).
- Phụ thuộc một app bên thứ ba phải đang chạy — thêm một bước trong quy trình studio.
- Cần laptop Windows/macOS tại buổi chụp.

Chi tiết implement cần chú ý:
- File RAW được ghi dần. **Phải đợi file ghi xong** trước khi đọc — theo dõi kích thước ổn định trong ~500ms, hoặc chờ sự kiện rename nếu NX Tether ghi qua file tạm. Đọc sớm sẽ ra preview hỏng.
- Nếu bật RAW+JPEG, sẽ có hai sự kiện cho cùng một tấm. Ghép chúng theo tên file gốc.

## Lợi thế màu: Picture Control

Đây là phần thú vị nhất khi chọn Nikon, và nên là một trụ cột của sản phẩm.

**Flexible Color Picture Control** cho phép tạo profile màu tùy biến với Color Blender và Color Grading trong NX Studio / Picture Control Utility 2, xuất ra file, nạp vào thân máy qua thẻ nhớ (`Manage Picture Control → Load/Save → Copy to Camera`).

Định dạng theo body:
- `.NP3` — các implementation mới nhất, Custom Picture Control trong máy
- `.NP2` và `.NCP` — Z-mount, D6, D5, D500, D850, D810, D780, D750, D7500, D7200, D5500/D5600
- `.NCP` — các DSLR đời cũ hơn

**Vì sao điều này quan trọng với sản phẩm:** nếu màu của người dùng đã được nạp vào máy, thì JPEG ra khỏi máy đã đúng màu, **và JPEG preview nhúng trong file NEF cũng đã đúng màu**. Nghĩa là preview tức thì trong app đã mang đúng look của họ mà không tốn một phép tính nào. Đây là thứ không làm sạch sẽ được trên hãng khác.

Chiến lược màu hai nhánh cho Nikon:
1. **Trong máy** — phát hành look của người dùng dưới dạng Custom Picture Control để nạp vào body. Preview tức thì đúng màu, miễn phí.
2. **Trong app** — LUT trên GPU như thường lệ, cho file RAW và cho việc đổi look sau khi chụp.

Giới hạn phải biết trước:
- **`.NP2`/`.NP3` là định dạng nhị phân độc quyền, không có tài liệu công khai.** Không sinh được bằng code nếu không reverse-engineer. Thực tế: tạo thủ công trong NX Studio rồi đóng gói phát hành kèm app. Đủ cho thư viện preset có sẵn, **không** đủ cho luồng "người dùng tạo màu trong app rồi tự xuất ra máy ảnh". Đừng hứa tính năng đó.
- Picture Control chỉ áp dụng ở **tone mode SDR** — không hoạt động với N-Log hay HLG.
- Adobe Lightroom không nhận diện được Flexible Color profile để camera-matching, nên workflow của khách có thể lệch nếu họ còn dùng Lightroom song song.

**Nikon Imaging Cloud** phát hành "Imaging Recipes" (Cloud Picture Control) tới ZR, Z6III, Z50II, Z5II, Zf — gồm 9 recipe hợp tác với RED mô phỏng LUT điện ảnh. Đây vừa là đối thủ gián tiếp (Nikon phát màu miễn phí), vừa là bằng chứng thị trường: nhu cầu "look sẵn theo phong cách" là thật, và người dùng Nikon đã quen với khái niệm này. Chưa thấy API công khai cho bên thứ ba phát hành recipe lên Imaging Cloud — kiểm chứng lại nếu chiến lược sản phẩm phụ thuộc vào nó.

## Ràng buộc thực tế của dự án này

Chốt 2026-08-23: **không dùng laptop/thiết bị trung gian, và iOS bắt buộc.** Cộng với Nikon, đây là tổ hợp đắt nhất có thể. Ba ràng buộc này loại sạch mọi đường rẻ:

| Đường | Vì sao bị loại |
|---|---|
| Hot-folder + NX Tether | Cần máy Windows/macOS |
| Nikon SDK | Chỉ Windows/macOS |
| Go agent + libgphoto2 | Cần một máy tính |
| Android USB PTP | Không giải quyết iOS |
| **ImageCaptureCore của Apple** | **Hỏng với app bên thứ ba từ iOS 18** — xem bên dưới |
| SnapBridge | 2MP auto (8MP trên Z đời mới), **không có RAW** |

Còn lại đúng **một** đường kỹ thuật: **CascableCore** — đã chốt. Kiến trúc chi tiết ở `ios-cascablecore.md`.

### ImageCaptureCore — cái bẫy hấp dẫn

`ImageCaptureCore` là framework của chính Apple, có trên iOS từ 13.0 (`ICDeviceBrowser`, `ICCameraDevice`), duyệt và tải ảnh từ camera PTP qua USB hoặc mạng, cần `NSCameraUsageDescription`. Nghe như lời giải miễn phí.

**Nhưng từ iOS 18, nhiều lập trình viên báo cáo `ICDeviceBrowser` không tìm thấy thiết bị nào trong app bên thứ ba** — trong khi app Photos của Apple vẫn import bình thường. Trước đó iOS 17 cũng từng phá vỡ tethering USB.

Cách xử lý đúng: **coi đây là giả thuyết phải spike, không phải nền móng.** Bỏ 2–3 ngày viết một app Swift tối giản chỉ để dựng `ICDeviceBrowser` và thử với đúng body + đúng bản iOS. Nếu chạy, tiết kiệm được license. Nếu không, đã biết sớm với chi phí rẻ. Đừng lập kế hoạch sản phẩm dựa trên nó trước khi spike xong.

### CascableCore — đường duy nhất còn lại

- Phủ Canon, Fujifilm, GoPro, **Nikon**, Olympus/OM System, Panasonic, Sony — 250+ model, cả WiFi lẫn USB, một API duy nhất.
- Nền tảng: **iOS, iPadOS, macOS, visionOS. Không có Android.**
- Có **bản dùng thử đầy đủ tính năng 30 ngày** — đây là hành động đầu tiên phải làm.
- Giá **thương lượng theo từng ứng dụng**, và Cascable nói rõ là dành cho sản phẩm/công ty đạt "partnership criteria" — nghĩa là **họ có quyền từ chối**. Phải liên hệ sớm, đừng để đến lúc gần ra mắt.

Hệ quả kiến trúc phải nói thẳng: **React Native không còn là cross-platform ở tầng capture.** iOS đi CascableCore (native module Swift), Android sẽ cần một implementation hoàn toàn khác (libgphoto2 NDK) hoặc ra mắt không có tethering. Đây là chi phí thật, phải đưa vào kế hoạch chứ không giả vờ là chi tiết nhỏ.

### SnapBridge — vì sao không đủ, và khi nào lại đủ

Giới hạn cứng: **không hỗ trợ RAW, TIFF, video.** Auto-download chỉ 2MP; một số Z đời mới cho 8MP ở chế độ AP. Chọn "original format" vẫn ra JPEG 2MP.

Với sản phẩm kiểu Evoto (chỉnh RAW chuyên nghiệp) thì không đủ — thiếu RAW là mất luôn khả năng regrade chất lượng cao.

Nhưng nếu định vị sản phẩm là **"khách xem ảnh đã lên màu ngay tại buổi chụp"**, thì 8MP JPEG là thừa đủ. Và khi Picture Control tùy biến đã nạp sẵn trong thân máy, những file JPEG đó **đã đúng màu của người dùng ngay khi rời máy ảnh**. Đây là con đường duy nhất chạy được ngay hôm nay, trên cả hai nền tảng, không license, không laptop. Đáng cân nhắc nghiêm túc như một định vị sản phẩm, không phải như một phương án chữa cháy.

## Phase 0 bắt buộc

Trước khi viết code sản phẩm, dành 1–2 tuần kiểm chứng trên **đúng body và đúng firmware** mà khách hàng dùng. Hành vi Nikon khác nhau rõ rệt giữa các model và giữa các bản firmware, nên mọi giả định đều phải đo, không suy luận.

Danh sách kiểm tra:
- [ ] NX Tether có bắt được body này qua USB không? Ghi file ra thư mục đúng như mong đợi?
- [ ] `gphoto2 --capture-tethered` có chạy không? Lỗi gì?
- [ ] Body nhận định dạng Picture Control nào — `.NP3`, `.NP2`, hay `.NCP`?
- [ ] JPEG preview nhúng trong NEF có kích thước bao nhiêu? Có phản ánh Picture Control đang bật không?
- [ ] Body có trong danh sách hỗ trợ của bản Nikon SDK hiện tại không?
- [ ] Đọc license agreement của Nikon SDK — có cho phép dùng thương mại theo mô hình dự định không?

Mục cuối là mục dễ bị bỏ qua nhất và tốn kém nhất nếu phát hiện muộn.
