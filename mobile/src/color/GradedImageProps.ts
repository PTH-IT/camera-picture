import type { StyleProp, ViewStyle } from 'react-native';
import type { ColorAdjustments } from './adjustments';

/** Một preset màu, ở dạng mà cả hai nền tảng đều dùng được. */
export interface PresetVisual {
  id: string;
  name: string;
  /** Đường dẫn ảnh hald 512x512, sinh bằng backend/cmd/lutconv. Dùng trên máy thật. */
  lutUri?: string | null;
  /** Chuỗi CSS filter xấp xỉ, CHỈ dùng cho bản xem trước trên trình duyệt. */
  webFilter?: string;
}

/**
 * Props dùng chung cho cả bản native lẫn bản web của GradedImage.
 *
 * Cố ý để chung một kiểu: nếu hai bản có chữ ký khác nhau, mọi màn hình gọi
 * chúng sẽ phải rẽ nhánh theo nền tảng, và loại rẽ nhánh đó lan rất nhanh.
 * Mỗi bản chỉ dùng phần nó cần trong PresetVisual.
 */
export interface GradedImageProps {
  /** Với file RAW, đây là JPEG preview nhúng — KHÔNG decode RAW trên điện thoại.
   *  Xem docs/adr/0001-capture-strategy.md. */
  uri: string;
  preset?: PresetVisual | null;
  /** Cường độ 0..1. Khớp tham số `amount` của lut.Apply phía Go, nên slider trên
   *  app và bản render của server cho cùng kết quả. */
  amount?: number;
  /**
   * Chỉnh màu thủ công. Bỏ trống nghĩa là không đổi gì.
   *
   * Tách khỏi `preset` vì hai thứ khác bản chất: preset là look dùng lại cho cả
   * buổi chụp, còn đây là hiệu chỉnh của riêng một tấm.
   */
  adjustments?: ColorAdjustments | null;
  width: number;
  height: number;
  style?: StyleProp<ViewStyle>;
  /** Nhãn nhắc màu là xấp xỉ. Chỉ có tác dụng ở bản web. */
  showApproximationBadge?: boolean;
}
