import { StyleSheet, View, type StyleProp, type ViewStyle } from 'react-native';
import {
  Canvas,
  Fill,
  Shader,
  ImageShader,
  FilterMode,
  MipmapMode,
  useImage,
} from '@shopify/react-native-skia';
import { getLutEffect } from './haldLut';
import type { GradedImageProps } from './GradedImageProps';

/**
 * Hiển thị ảnh đã áp LUT, xử lý hoàn toàn trên GPU.
 *
 * Bản chạy trên máy thật. Bản `.web.tsx` bên cạnh dùng cho bản xem trước trên
 * trình duyệt và KHÔNG cho cùng kết quả màu.
 *
 * Hai bản dùng CHUNG một bộ props (xem GradedImageProps) để màn hình gọi chúng
 * không cần biết mình đang chạy ở đâu. Nếu để chữ ký khác nhau, mọi màn hình sẽ
 * phải rẽ nhánh theo nền tảng — và đó là thứ lan ra rất nhanh.
 */
export function GradedImage({
  uri,
  preset,
  amount = 1,
  width,
  height,
  style,
}: GradedImageProps) {
  const image = useImage(uri);
  const lut = useImage(preset?.lutUri ?? null);
  const effect = getLutEffect();
  const clamped = Math.min(1, Math.max(0, amount));

  // useImage trả null trong lúc đang tải. Vẽ ô trống thay vì để Canvas nhận
  // image null — nếu không, khung hình sẽ nháy đen mỗi lần đổi ảnh.
  if (!image) {
    return <View style={[{ width, height, backgroundColor: '#1a1a1d' }, style]} />;
  }

  return (
    <View style={[{ width, height }, style]}>
      <Canvas style={StyleSheet.absoluteFill}>
        <Fill>
          <Shader source={effect} uniforms={{ amount: lut ? clamped : 0 }}>
            <ImageShader image={image} fit="cover" rect={{ x: 0, y: 0, width, height }} />
            {/* Khi chưa có LUT vẫn phải truyền một ảnh vào con thứ hai: shader
                khai hai `uniform shader`, thiếu một cái là biên dịch lỗi. Truyền
                lại chính ảnh nguồn và đặt amount = 0 để nó không có tác dụng. */}
            <ImageShader
              image={lut ?? image}
              tx="clamp"
              ty="clamp"
              sampling={{ filter: FilterMode.Linear, mipmap: MipmapMode.None }}
            />
          </Shader>
        </Fill>
      </Canvas>
    </View>
  );
}

export type { GradedImageProps, StyleProp, ViewStyle };
