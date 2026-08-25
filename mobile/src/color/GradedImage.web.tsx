import { Image, StyleSheet, Text, View } from 'react-native';
import type { GradedImageProps } from './GradedImageProps';

/**
 * Bản dùng cho BẢN XEM TRƯỚC TRÊN TRÌNH DUYỆT.
 *
 * Màu ở đây là XẤP XỈ bằng CSS filter và KHÔNG khớp với bản chạy trên máy thật,
 * vốn dùng LUT hald 3D trong shader Skia. Khác biệt là có chủ ý và không có kế
 * hoạch làm cho khớp: bản xem trước để kiểm tra bố cục và luồng thao tác, không
 * phải để đánh giá màu.
 *
 * Nhãn trên ảnh tồn tại để không ai nhìn màn hình này rồi kết luận về màu.
 */
export function GradedImage({
  uri,
  preset,
  amount = 1,
  width,
  height,
  style,
  showApproximationBadge = true,
}: GradedImageProps) {
  const clamped = Math.min(1, Math.max(0, amount));
  const filter = preset?.webFilter ?? 'none';
  const graded = filter !== 'none' && clamped > 0;

  return (
    <View style={[{ width, height, overflow: 'hidden' }, style]}>
      <Image source={{ uri }} style={[StyleSheet.absoluteFillObject, { width, height }]} />
      {graded ? (
        <Image
          source={{ uri }}
          style={[
            StyleSheet.absoluteFillObject,
            { width, height, opacity: clamped },
            // @ts-expect-error `filter` chỉ tồn tại ở react-native-web
            { filter },
          ]}
        />
      ) : null}
      {showApproximationBadge ? (
        <View style={s.badge} pointerEvents="none">
          <Text style={s.badgeText}>màu xấp xỉ · bản xem trước</Text>
        </View>
      ) : null}
    </View>
  );
}

const s = StyleSheet.create({
  badge: {
    position: 'absolute',
    // Góc phải dưới: góc trái dưới là chỗ app đặt nhãn trạng thái ảnh, và nhãn
    // của bản xem trước không được che nội dung thật.
    right: 8,
    bottom: 8,
    backgroundColor: 'rgba(0,0,0,0.55)',
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 999,
  },
  badgeText: { fontSize: 10, color: 'rgba(255,255,255,0.75)' },
});
