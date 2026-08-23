# Định dạng hald LUT — nguồn sự thật duy nhất

File này là hợp đồng giữa **shader trên thiết bị** (`mobile/src/color/haldLut.ts`) và
**bộ render phía server** (`backend/internal/imaging/lut/`). Hai bên phải cho cùng
kết quả trên cùng một LUT; nếu lệch, người dùng sẽ thấy preview đẹp rồi file xuất
ra khác màu, và sẽ mất niềm tin ngay lần đầu.

Sửa bất cứ hằng số nào ở đây thì phải sửa cả hai bên **và** chạy lại test đối chiếu.

## Vì sao không dùng thẳng `.cube`

`.cube` là định dạng văn bản mà colorist xuất ra từ Resolve hay Lightroom. GPU không
đọc được file text, nên LUT 3D phải được trải phẳng thành một ảnh 2D để nạp làm
texture. `.cube` vẫn là **nguồn**; hald là **dạng vận chuyển cho GPU**.

## Quy ước layout

| Tham số | Giá trị | Ký hiệu |
|---|---|---|
| Độ phân giải mỗi trục | 64 | `N` |
| Số tile mỗi hàng | 8 | `T` |
| Kích thước ảnh | 512 × 512 | `N*T` |
| Số tile | 64 (8×8) | một tile cho mỗi mức blue |

Ánh xạ:

```
tile b  →  (tx, ty) = (b % T, b / T)      với b = mức blue, 0..63
trong tile:  x = mức red,  y = mức green  (0..63)
pixel:  px = tx*N + r      py = ty*N + g
```

**Chọn 512×512 / 8×8 là có chủ đích.** Có báo cáo thực tế rằng LUT 1024×1024 với
16×16 tile gây trục trặc khi sample trong Skia runtime effect. 512×512 là cấu hình
được dùng thành công rộng rãi nhất. Nếu gặp lỗi sample lạ, hạ về cấu hình này
trước khi nghi ngờ chỗ khác.

## Công thức tra cứu

Ba điểm dưới đây là nơi các implementation tự viết hay sai. Cả ba đã được kiểm
chứng bằng mô phỏng bilinear của GPU trên 2010 mẫu màu; identity LUT round-trip
với sai số `5.6e-16`.

### 1. Nội suy trục blue là bắt buộc

Blue chọn tile, nên nó **không** được texture filtering nội suy giúp. Phải lấy hai
tile kề nhau rồi `mix` thủ công:

```
blue = b * (N-1)
b0 = floor(blue),  b1 = min(b0+1, N-1),  f = blue - b0
kết quả = mix(sample(tile b0), sample(tile b1), f)
```

Bỏ bước này sẽ gây **banding rõ rệt ở vùng gradient** — trời và da là hai chỗ lộ
nhất, và cũng là hai chỗ quan trọng nhất với ảnh chân dung.

### 2. Red/green phải nằm giữa tâm texel đầu và tâm texel cuối

```
rx = 0.5 + r * (N-1)     → khoảng [0.5, 63.5]
gy = 0.5 + g * (N-1)
```

Không phải `r * N` hay `r * (N-1)`. Lý do: tâm texel `i` nằm ở `i + 0.5`. Công thức
trên đảm bảo mọi sample rơi hẳn **trong** tile, kể cả ở hai đầu. Sai ở đây thì
bilinear filtering sẽ **rò màu từ tile bên cạnh** — mà tile bên cạnh là một mức
blue hoàn toàn khác, nên lỗi biểu hiện thành những vệt màu sai ở vùng có red hoặc
green gần 0 hoặc gần 1.

### 3. Texture phải dùng linear filtering

Nội suy trên trục red/green được **giao cho phần cứng** làm qua bilinear filtering.
Nếu nạp LUT với nearest filtering, LUT sẽ bị lượng tử hoá thành 64 mức mỗi trục và
xuất hiện posterization.

Đồng thời LUT PNG phải được nạp **không qua color management và không premultiply
alpha**. Nếu hệ điều hành tự chuyển color space của PNG, LUT sẽ sai một cách âm
thầm — ảnh vẫn trông "có màu", chỉ là sai.

## Về độ chính xác: hai dạng, một phép toán

| | Thiết bị | Server |
|---|---|---|
| Dạng LUT | hald PNG 8-bit, luôn 64³ | `.cube` ở float, giữ nguyên độ phân giải gốc |
| Nội suy | trilinear (bilinear phần cứng + mix blue) | trilinear, cùng công thức |

Hai đường cố ý **không** dùng chung một dạng lưu trữ: bắt server render bản giao
khách qua LUT 8-bit là tự bỏ đi chất lượng không có lý do.

Có hai nguồn sai lệch giữa chúng:

1. **Lượng tử hoá 8-bit** của entry hald — giới hạn lý thuyết 0,5 mức.
2. **Khác lưới**: hald luôn là 64³, còn server dùng lưới gốc (17³, 33³, 65³...).
   Nội suy trilinear không bất biến qua việc lấy mẫu lại, nên phần này khác 0.

Phương án "cho server cũng lấy mẫu lại về 64³ để hai lưới trùng nhau" **đã được
cân nhắc và bác bỏ**. Đo trên 17³, 32³, 33³, 65³, gồm cả creative look có độ cong
cao và split toning: bỏ bước lấy mẫu lại chỉ làm sai lệch tăng từ 0,49 lên 0,67
mức 8-bit. Đổi lại, lấy mẫu lại một LUT 17³ lên 64³ làm số entry tăng 53 lần. Không
đáng — 0,2 mức 8-bit nằm xa dưới ngưỡng nhìn thấy được.

Điều này được **ép buộc bằng test**, không phải bằng lời hứa: `lut_test.go` đặt
ngưỡng 1 mức 8-bit (dư địa ~33% so với số đo xấu nhất). Nếu một LUT thật của khách
làm test vỡ, cách xử lý là **nâng độ phân giải hald**, không phải lấy mẫu lại phía
server — vì bản thân việc lệch lưới không phải nguyên nhân gốc, độ phân giải hald
mới là.

## Khi nào phải xem lại tài liệu này

- Đổi `N` hoặc `T` → sửa cả hai bên, chạy lại test đối chiếu
- Chuyển sang LUT có domain khác `[0,1]` → phần `DOMAIN_MIN`/`DOMAIN_MAX` của
  `.cube` hiện đang được áp dụng lúc parse; nếu chuyển sang xử lý ở shader thì
  phải thêm uniform
- Chuyển sang làm việc ở không gian linear thay vì sRGB-encoded → **đây là thay
  đổi lớn**, phải quyết một lần cho cả hai đường và ghi vào ADR
