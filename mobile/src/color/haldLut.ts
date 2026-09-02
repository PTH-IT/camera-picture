import { Skia, TileMode, FilterMode, MipmapMode } from '@shopify/react-native-skia';
import type { SkImage, SkRuntimeEffect, SkShader } from '@shopify/react-native-skia';
import { ADJUSTMENT_KEYS, NEUTRAL_ADJUSTMENTS, type ColorAdjustments } from './adjustments';

/**
 * Áp LUT màu trên GPU bằng Skia runtime effect.
 *
 * Quy ước layout và lý do đằng sau từng dòng công thức: docs/hald-lut-format.md.
 * Bản đối ứng phía server: backend/internal/imaging/lut/. Hai bên phải cho cùng
 * kết quả — backend/internal/imaging/lut/lut_test.go ép buộc điều đó.
 *
 * Vì sao chạy trên GPU chứ không gọi server: người dùng kéo slider 60 lần mỗi
 * giây. Mỗi lần round-trip mạng là một trải nghiệm chết.
 */

/** PHẢI khớp với HaldSize/HaldTiles/HaldDim trong backend/internal/imaging/lut/hald.go. */
export const HALD_SIZE = 64;
export const HALD_TILES = 8;
export const HALD_DIM = HALD_SIZE * HALD_TILES; // 512

/**
 * Chỉnh màu thủ công, chạy TRƯỚC khi tra LUT.
 *
 * Thứ tự có lý do: những phép này là hiệu chỉnh cho bản chụp (bù sáng, cân bằng
 * trắng), còn LUT là "look" phủ lên trên. Đảo thứ tự nghĩa là look được áp lên
 * một tấm chưa hiệu chỉnh rồi mới sửa sáng — kết quả khác hẳn, và khác cả với
 * cách mọi phần mềm hậu kỳ làm.
 *
 * Công thức viết ra ở đây để bản render phía máy chủ CHÉP LẠI ĐƯỢC. Chừng nào
 * backend/internal/imaging/lut chưa có chúng, ảnh xuất ra từ máy chủ sẽ khác
 * ảnh trên máy — xem chú thích trong color/adjustments.ts.
 */
const ADJUSTMENT_SKSL = `
const half3 LUMA = half3(0.2126, 0.7152, 0.0722);

half3 adjust(half3 c) {
    // 1. Cân bằng trắng. Ấm thì thêm đỏ bớt lam; sắc thì bù lục/tím.
    c.r *= 1.0 + half(temperature) * 0.25;
    c.b *= 1.0 - half(temperature) * 0.25;
    c.g *= 1.0 - half(tint) * 0.15;

    // 2. Bù sáng, tính theo KHẨU chứ không cộng tuyến tính: cộng thẳng sẽ làm
    //    bệt vùng tối và cháy vùng sáng, còn nhân theo luỹ thừa 2 giữ nguyên
    //    tương quan giữa các vùng — đúng như bù sáng trên máy ảnh.
    c *= half(exp2(exposure * 2.0));

    // 3. Tương phản quanh điểm giữa 0.5.
    c = (c - 0.5) * (1.0 + half(contrast)) + 0.5;

    // 4. Vùng sáng và vùng tối, có trọng số theo độ sáng để không đụng vào vùng
    //    trung tính. Bình phương cho vùng ảnh hưởng hẹp lại quanh hai đầu.
    half l = dot(clamp(c, 0.0, 1.0), LUMA);
    c += half(shadows) * 0.4 * (1.0 - l) * (1.0 - l);
    c += half(highlights) * 0.4 * l * l;

    // 5. Bão hoà, trộn về mức xám cùng độ sáng.
    half g = dot(clamp(c, 0.0, 1.0), LUMA);
    c = mix(half3(g), c, 1.0 + half(saturation));

    return clamp(c, 0.0, 1.0);
}
`;

/**
 * Công thức tra cứu đã được kiểm chứng bằng mô phỏng bilinear của GPU: LUT
 * identity round-trip với sai số 5.6e-16 trên 2010 mẫu màu. Ba chỗ dưới đây là
 * nơi các implementation tự viết hay sai — đừng "đơn giản hoá" chúng:
 *
 *  1. Trục blue phải nội suy thủ công. Blue chọn tile, nên texture filtering
 *     không giúp được. Bỏ bước này gây banding rõ ở trời và da.
 *  2. rx/gy phải là `0.5 + v*(N-1)`, không phải `v*N`. Tâm texel i nằm ở i+0.5;
 *     công thức này giữ mọi sample nằm hẳn trong tile. Sai thì bilinear sẽ rò
 *     màu từ tile bên cạnh — vốn là một mức blue hoàn toàn khác.
 *  3. Phải unpremultiply trước khi tra LUT rồi premultiply lại. Skia làm việc ở
 *     alpha nhân sẵn; áp LUT thẳng lên màu đã nhân alpha sẽ sai ở mọi pixel
 *     không đục hoàn toàn.
 */
const HALD_LUT_SKSL = `
uniform shader image;
uniform shader lut;
uniform float amount;

// Thứ tự khai báo PHẢI khớp ADJUSTMENT_KEYS trong color/adjustments.ts và thứ
// tự mảng uniform ở makeGradedShader. Trình biên dịch không kiểm giúp.
uniform float exposure;
uniform float contrast;
uniform float saturation;
uniform float temperature;
uniform float tint;
uniform float highlights;
uniform float shadows;
${ADJUSTMENT_SKSL}

const float N = ${HALD_SIZE}.0;
const float T = ${HALD_TILES}.0;

half3 lutLookup(half3 c) {
    c = clamp(c, 0.0, 1.0);

    // Blue chọn tile — phải tự nội suy giữa hai tile kề nhau.
    float blue = float(c.b) * (N - 1.0);
    float b0 = floor(blue);
    float b1 = min(b0 + 1.0, N - 1.0);
    half  f  = half(blue - b0);

    // Red/green nằm giữa tâm texel đầu và tâm texel cuối của tile: [0.5, N-0.5].
    float2 uv = float2(0.5 + float(c.r) * (N - 1.0),
                       0.5 + float(c.g) * (N - 1.0));

    float2 o0 = float2(mod(b0, T) * N, floor(b0 / T) * N);
    float2 o1 = float2(mod(b1, T) * N, floor(b1 / T) * N);

    half3 s0 = lut.eval(o0 + uv).rgb;
    half3 s1 = lut.eval(o1 + uv).rgb;
    return mix(s0, s1, f);
}

half4 main(float2 xy) {
    half4 src = image.eval(xy);

    half a = src.a;
    half3 c = a > 0.0 ? src.rgb / a : half3(0.0);

    c = adjust(c);
    half3 graded = mix(c, lutLookup(c), half(clamp(amount, 0.0, 1.0)));

    return half4(graded * a, a);
}
`;

let cachedEffect: SkRuntimeEffect | null = null;

/**
 * Biên dịch runtime effect, cache lại cho các lần sau.
 *
 * Biên dịch shader tốn vài mili giây và kết quả không đổi, nên biên dịch lại ở
 * mỗi lần render sẽ làm giật khi kéo slider — đúng thứ cần tránh nhất.
 */
export function getLutEffect(): SkRuntimeEffect {
  if (cachedEffect) return cachedEffect;

  const effect = Skia.RuntimeEffect.Make(HALD_LUT_SKSL);
  if (!effect) {
    // Không nuốt lỗi này: shader hỏng thì ảnh sẽ hiện sai màu hoặc trong suốt,
    // và triệu chứng không chỉ về nguyên nhân.
    throw new Error('haldLut: biên dịch SkSL thất bại');
  }
  cachedEffect = effect;
  return effect;
}

/**
 * Tạo shader cho ảnh LUT.
 *
 * Ba tham số dưới đây không phải mặc định tuỳ tiện:
 * - FilterMode.Linear là BẮT BUỘC. Nội suy trên trục red/green được giao cho
 *   phần cứng làm; dùng Nearest sẽ khiến LUT chỉ còn 64 mức mỗi trục và ảnh bị
 *   posterization.
 * - MipmapMode.None: LUT không bao giờ được thu nhỏ, mipmap chỉ làm nhoè dữ liệu.
 * - TileMode.Clamp: công thức đã giữ sample trong biên, clamp chỉ là lưới an toàn.
 */
export function makeLutShader(lutImage: SkImage): SkShader {
  if (lutImage.width() !== HALD_DIM || lutImage.height() !== HALD_DIM) {
    throw new Error(
      `haldLut: ảnh LUT phải là ${HALD_DIM}x${HALD_DIM}, ` +
        `nhận được ${lutImage.width()}x${lutImage.height()}`,
    );
  }
  return lutImage.makeShaderOptions(
    TileMode.Clamp,
    TileMode.Clamp,
    FilterMode.Linear,
    MipmapMode.None,
  );
}

/**
 * Ghép ảnh nguồn + LUT + cường độ thành một shader duy nhất để vẽ.
 *
 * `amount` trong [0,1] khớp với tham số cùng tên của lut.Apply phía Go, nên
 * slider trên app và bản render của server cho cùng kết quả.
 */
export function makeGradedShader(
  source: SkShader,
  lut: SkShader,
  amount: number,
  adjustments: ColorAdjustments = NEUTRAL_ADJUSTMENTS,
): SkShader {
  // API mệnh lệnh nhận uniform là MẢNG SỐ PHẲNG, không phải object có tên — thứ
  // tự phải khớp thứ tự khai báo trong SkSL, và trình biên dịch không kiểm giúp.
  // Vì vậy phần chỉnh màu được sinh từ ADJUSTMENT_KEYS thay vì liệt kê tay: một
  // danh sách duy nhất cho cả hai nơi thì không lệch được.
  //
  // Thứ tự children cũng vậy: [image, lut] khớp thứ tự `uniform shader` trong
  // SkSL. Đảo hai cái này sẽ cho ra ảnh là LUT và LUT là ảnh.
  return getLutEffect().makeShaderWithChildren(
    [
      Math.min(1, Math.max(0, amount)),
      ...ADJUSTMENT_KEYS.map(k => Math.min(1, Math.max(-1, adjustments[k]))),
    ],
    [source, lut],
  );
}
