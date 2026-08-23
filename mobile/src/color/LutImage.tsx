import React, { useMemo } from 'react';
import {
  Canvas,
  Fill,
  Shader,
  ImageShader,
  type SkImage,
} from '@shopify/react-native-skia';
import { getLutEffect, HALD_DIM } from './haldLut';

export interface LutImageProps {
  /** Ảnh cần hiển thị. Với ảnh RAW, đây là JPEG preview nhúng, không phải bản decode. */
  image: SkImage;
  /** Ảnh hald 512×512. Sinh bằng `backend/cmd/lutconv`. */
  lut: SkImage;
  /** Cường độ trong [0,1]. Khớp với tham số `amount` của lut.Apply phía Go. */
  amount?: number;
  width: number;
  height: number;
}

/**
 * Hiển thị ảnh đã áp LUT, xử lý hoàn toàn trên GPU.
 *
 * Vì sao không dùng `<Image>` của React Native: qua RN Image thì không kiểm soát
 * được color pipeline — hệ điều hành có thể tự chuyển color space, và không có
 * chỗ nào để chèn shader. Mọi ảnh đang được chỉnh màu phải đi qua Skia.
 *
 * Vì sao pixel không đi qua JS: một NEF là 50–60MB. `SkImage` giữ dữ liệu ở phía
 * native; JS chỉ cầm tham chiếu. Đây là luật số 2 trong README.
 */
export function LutImage({ image, lut, amount = 1, width, height }: LutImageProps) {
  if (__DEV__ && (lut.width() !== HALD_DIM || lut.height() !== HALD_DIM)) {
    // Nạp nhầm ảnh thường vào chỗ chờ LUT cho ra màu hỏng kỳ quái mà không có
    // thông báo nào — kiểm tra sớm ở dev để khỏi mất hàng giờ truy vết.
    throw new Error(
      `LutImage: ảnh LUT phải là ${HALD_DIM}x${HALD_DIM}, ` +
        `nhận được ${lut.width()}x${lut.height()}`,
    );
  }

  const effect = useMemo(() => getLutEffect(), []);
  const uniforms = useMemo(
    () => ({ amount: Math.min(1, Math.max(0, amount)) }),
    [amount],
  );

  return (
    <Canvas style={{ width, height }}>
      <Fill>
        <Shader source={effect} uniforms={uniforms}>
          {/* Con thứ nhất — uniform `image`. `fit=contain` đặt local matrix để
              ảnh vừa khung; toạ độ shader nhận được đã qua phép biến đổi này. */}
          <ImageShader
            image={image}
            fit="contain"
            rect={{ x: 0, y: 0, width, height }}
          />

          {/* Con thứ hai — uniform `lut`. KHÔNG đặt rect/fit: shader cần toạ độ
              pixel thô của ảnh 512×512. Thêm fit vào đây sẽ làm lệch toàn bộ
              phép tra cứu.

              fm="linear" là bắt buộc — nội suy trên trục red/green được giao cho
              phần cứng. Xem docs/hald-lut-format.md mục 3. */}
          <ImageShader image={lut} tx="clamp" ty="clamp" fm="linear" mm="none" />
        </Shader>
      </Fill>
    </Canvas>
  );
}
