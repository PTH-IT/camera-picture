import { ImageFormat, Skia, TileMode, FilterMode, MipmapMode } from '@shopify/react-native-skia';
import { makeGradedShader, makeLutShader } from '../color/haldLut';
import { NEUTRAL_ADJUSTMENTS, type ColorAdjustments } from '../color/adjustments';
import type { PresetVisual } from '../color/GradedImageProps';

/**
 * Kết xuất ảnh đã áp màu ra JPEG, chạy trên GPU của thiết bị.
 *
 * Dùng ĐÚNG shader mà màn hình đang hiển thị (`makeGradedShader`), nên thứ người
 * dùng nhìn thấy và thứ họ nhận được là một. Viết một đường kết xuất riêng cho
 * việc xuất file là cách chắc chắn để hai bên trôi khỏi nhau theo thời gian.
 *
 * Kích thước bằng đúng ảnh nguồn. Ảnh nguồn ở đây là JPEG preview nhúng trong
 * RAW (~2MP) — đủ để gửi khách xem tại chỗ, KHÔNG phải bản in. Bản chất lượng
 * cao phải kết xuất từ RAW, và RAW còn nằm trên thẻ nhớ
 * (docs/adr/0001-capture-strategy.md).
 */
export interface RenderedImage {
  base64: string;
  width: number;
  height: number;
  /** Số byte thật của JPEG, để khai đúng với nơi lưu trữ. */
  byteSize: number;
}

export async function renderGraded(
  uri: string,
  preset: PresetVisual | null | undefined,
  amount: number,
  adjustments: ColorAdjustments = NEUTRAL_ADJUSTMENTS,
  quality = 92,
): Promise<RenderedImage> {
  const data = await Skia.Data.fromURI(uri);
  const image = Skia.Image.MakeImageFromEncoded(data);
  if (!image) throw new Error('không đọc được ảnh nguồn');

  const width = image.width();
  const height = image.height();

  const surface = Skia.Surface.MakeOffscreen(width, height);
  if (!surface) throw new Error('không tạo được vùng vẽ ngoài màn hình');

  // LUT là tuỳ chọn, nhưng shader luôn khai hai `uniform shader`. Không có LUT
  // thì truyền lại chính ảnh nguồn và đặt amount = 0 — giống hệt GradedImage.
  let lutShader = null;
  if (preset?.lutUri) {
    const lutData = await Skia.Data.fromURI(preset.lutUri);
    const lutImage = Skia.Image.MakeImageFromEncoded(lutData);
    if (lutImage) lutShader = makeLutShader(lutImage);
  }

  const imageShader = image.makeShaderOptions(
    TileMode.Clamp,
    TileMode.Clamp,
    FilterMode.Linear,
    MipmapMode.None,
  );

  const paint = Skia.Paint();
  paint.setShader(
    makeGradedShader(imageShader, lutShader ?? imageShader, lutShader ? amount : 0, adjustments),
  );

  surface.getCanvas().drawPaint(paint);
  surface.flush();

  const snapshot = surface.makeImageSnapshot();
  const base64 = snapshot.encodeToBase64(ImageFormat.JPEG, quality);

  return {
    base64,
    width,
    height,
    // base64 nở 4/3 so với dữ liệu gốc, trừ phần đệm '='. Tính ngược thay vì
    // decode lại: nơi lưu trữ chỉ cần con số, và decode một ảnh 2MP chỉ để đếm
    // byte là lãng phí.
    byteSize: Math.floor((base64.length * 3) / 4) - (base64.endsWith('==') ? 2 : base64.endsWith('=') ? 1 : 0),
  };
}
