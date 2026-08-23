# Pipeline màu — LUT, RAW, và chuyện giữ cho hai đường không lệch nhau

Mục lục:
- [Kiến trúc hai tốc độ](#kiến-trúc-hai-tốc-độ)
- [Định dạng LUT](#định-dạng-lut)
- [Áp LUT trên GPU trong React Native](#áp-lut-trên-gpu-trong-react-native)
- [Xử lý RAW](#xử-lý-raw)
- [Render bản final trong Go](#render-bản-final-trong-go)
- [Giữ hai đường khớp nhau](#giữ-hai-đường-khớp-nhau)
- [Preset là gì, và lưu thế nào](#preset-là-gì-và-lưu-thế-nào)

## Kiến trúc hai tốc độ

Người dùng kéo slider liên tục, nhưng chỉ xuất file vài lần. Hai nhu cầu này mâu thuẫn nhau về chi phí, nên tách hẳn:

**Đường nhanh (trên thiết bị)** — mục tiêu: dưới 16ms mỗi frame.
```
RAW/JPEG đến → trích JPEG preview nhúng (~2MP) → texture GPU
   → Skia RuntimeEffect(hald LUT + các tham số chỉnh) → hiển thị
```

**Đường chậm (server)** — mục tiêu: chất lượng, không phải tốc độ.
```
RAW gốc → libraw (demosaic, WB, exposure) → linear RGB
   → libvips (áp LUT, tone curve) → ICC transform → JPEG/TIFF xuất
```

Đừng gộp hai đường. Đừng gửi ảnh lên server chỉ để xem preview.

## Định dạng LUT

`.cube` là định dạng nguồn mà colorist tạo ra (Resolve, Lightroom export). Nhưng GPU shader không đọc file text được. Chuyển đổi:

- **`.cube` → hald image PNG**: LUT 3D kích thước N³ được trải phẳng thành ảnh 2D. Quy ước phổ biến: LUT 33³ hoặc 64³ → PNG 512×512 (8×8 tile, mỗi tile 64×64) hoặc 1024×1024 (16×16 tile).
- Trong RN, LUT được nạp như một `Skia.Image` bình thường rồi truyền vào shader qua uniform `shader`.

Có báo cáo thực tế là LUT 1024×1024 với 16×16 tile gây khó khi sample trong Skia runtime effect — nếu gặp trục trặc, **hạ xuống 512×512 / 8×8 trước khi nghi ngờ chỗ khác**. Đây là cấu hình được dùng thành công nhiều nhất.

Nên viết một tool Go nhỏ (`cmd/lutconv`) làm việc `.cube` → hald PNG, và dùng chính nó để sinh LUT cho cả app lẫn server. Một nguồn sự thật duy nhất là cách rẻ nhất để tránh lệch màu.

## Áp LUT trên GPU trong React Native

Thư viện: `@shopify/react-native-skia`, dùng `Skia.RuntimeEffect.Make(sksl)` và component `<RuntimeShader>` / `<ImageShader>`.

Cấu trúc shader (SkSL) cho hald LUT 8×8 tile, mỗi tile 64×64:

```glsl
uniform shader image;   // ảnh nguồn
uniform shader lut;     // hald PNG 512x512
uniform float amount;   // cường độ 0..1, để làm slider

half4 sampleLUT(half3 c) {
    const float SIZE = 64.0;    // độ phân giải mỗi chiều
    const float TILES = 8.0;    // số tile mỗi hàng
    float b = clamp(c.b, 0.0, 1.0) * (SIZE - 1.0);
    float b0 = floor(b);
    float b1 = min(b0 + 1.0, SIZE - 1.0);
    float f  = b - b0;          // nội suy tuyến tính theo trục blue

    half4 s0 = lut.eval(tileCoord(c.rg, b0, SIZE, TILES));
    half4 s1 = lut.eval(tileCoord(c.rg, b1, SIZE, TILES));
    return mix(s0, s1, f);
}
```

Ba điểm dễ sai:
1. **Nội suy trục blue là bắt buộc.** Bỏ qua nó sẽ gây banding rõ rệt ở vùng gradient (trời, da). Đây là lỗi phổ biến nhất khi tự viết LUT shader.
2. **Sample phải rơi vào giữa texel**, không phải cạnh — cộng offset `0.5/size` khi tính tọa độ, nếu không sẽ bị rò màu từ tile bên cạnh.
3. **LUT PNG phải nạp không qua color management và không premultiply alpha.** Nếu OS tự chuyển color space của PNG, LUT sẽ sai một cách âm thầm.

Uniform `amount` cho phép làm slider cường độ: `mix(originalColor, lutColor, amount)`.

## Xử lý RAW

**Đừng decode RAW trên điện thoại.** Vừa chậm, vừa tốn pin, vừa dễ OOM, và không cần thiết.

Mọi file RAW (NEF, CR3, ARW, RAF, DNG) đều nhúng sẵn JPEG preview do chính máy ảnh render — thường 1–2MP, đôi khi full-res. Trích nó ra:
- Trong Go: `libraw` có API lấy thumbnail; hoặc parse EXIF/TIFF IFD để tìm `PreviewImage`.
- `exiftool -b -JpgFromRaw` là cách nhanh để thử nghiệm và kiểm chứng.

Preview này chính là "ảnh máy ảnh render" mà nhiếp ảnh gia đã quen nhìn ở màn hình sau máy — nên dùng nó làm nền cho preview là lựa chọn đúng cả về kỹ thuật lẫn về kỳ vọng người dùng.

Khi cần render RAW thật (bản final):
- **libraw** (C++, qua cgo) — tiêu chuẩn công nghiệp, phủ gần hết máy ảnh, cập nhật nhanh khi có body mới.
- Thay thế: gọi `darktable-cli` hoặc `rawtherapee-cli` như subprocess. Chậm hơn nhưng chất lượng demosaic tốt và không phải viết cgo. Hợp lý cho MVP.

Lưu ý license: libraw có LGPL và bản thương mại. Kiểm tra điều khoản trước khi phát hành sản phẩm đóng.

## Render bản final trong Go

```
libraw: đọc RAW → set WB/exposure → demosaic → xuất 16-bit linear RGB
  → libvips (govips/bimg): áp LUT 3D, tone curve, sharpening, resize
  → ICC transform sang sRGB (hoặc Display P3 / Adobe RGB tùy đích)
  → encode JPEG/TIFF, ghi lại EXIF + bản quyền
```

libvips là lựa chọn đúng cho tầng này: streaming, dùng ít RAM với ảnh lớn, có sẵn ICC transform. Binding Go: `davidbyttow/govips` hoặc `h2non/bimg`.

Luôn làm việc ở **16-bit trở lên cho tới bước encode cuối**. Áp LUT trên dữ liệu 8-bit rồi mới chỉnh tiếp sẽ tạo posterization không cứu được.

## Giữ hai đường khớp nhau

Đây là chỗ dự án dễ mất niềm tin của người dùng nhất: preview trên máy đẹp, xuất file ra lại khác màu.

Bắt buộc phải có:
- **Test đối chiếu pixel.** Lấy một bộ ảnh chuẩn, chạy qua cả shader (render offscreen) lẫn pipeline Go, so sánh. Đặt ngưỡng ΔE (CIE2000) — thực tế ΔE < 2 là không phân biệt được bằng mắt.
- **Cùng một file LUT** cho cả hai đường, sinh từ cùng một tool.
- **Thống nhất transfer function.** Nếu shader làm việc trong sRGB-encoded còn Go làm trong linear, kết quả sẽ lệch rõ rệt ở vùng tối. Chọn một, ghi rõ vào tài liệu, và assert trong test.
- Sự khác biệt còn lại giữa "JPEG preview do máy ảnh render" và "RAW do libraw render" là có thật và không xóa được hoàn toàn. Cách xử lý trung thực: cho phép người dùng bấm "render chất lượng cao" trên ảnh đang xem để thấy đúng bản cuối, thay vì giả vờ chúng giống nhau.

## Preset là gì, và lưu thế nào

Preset của người dùng không chỉ là một LUT — nó là một tập tham số. Lưu dưới dạng JSON có version:

```json
{
  "version": 1,
  "name": "Wedding Warm",
  "lut": { "id": "warm-01", "amount": 0.85 },
  "basic": { "exposure": 0.2, "contrast": 8, "temp": 5600, "tint": 4 },
  "tone_curve": [[0,0],[64,58],[192,200],[255,255]],
  "hsl": { "orange": { "hue": -3, "sat": 12, "lum": 6 } },
  "grain": { "amount": 12, "size": 1.2 }
}
```

Đánh version ngay từ đầu. Preset là tài sản của người dùng, và họ sẽ giữ chúng nhiều năm — mọi thay đổi cấu trúc sau này phải migrate được, không được làm hỏng file cũ.
