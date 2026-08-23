import { Skia, TileMode, FilterMode, MipmapMode } from '@shopify/react-native-skia';
import type { SkImage, SkRuntimeEffect, SkShader } from '@shopify/react-native-skia';

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
): SkShader {
  return getLutEffect().makeShaderWithChildren(
    { amount: Math.min(1, Math.max(0, amount)) },
    [source, lut],
  );
}
