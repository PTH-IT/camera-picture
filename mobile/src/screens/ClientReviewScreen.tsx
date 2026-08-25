import { Pressable, StyleSheet, Text, useWindowDimensions } from 'react-native';
import { GradedImage } from '../color/GradedImage';
import { spacing, typography } from '../ui/theme';
import type { PresetView, ShotView } from './types';

/**
 * Chế độ khách xem.
 *
 * Đây là lúc nhiếp ảnh gia quay màn hình về phía khách hàng. Mọi thứ không phải
 * tấm ảnh đều bị bỏ đi: không tên file, không thông số, không nút cull, không
 * điểm sao. Khách không cần biết ảnh này là DSC_4821 hay nó bị đánh dấu loại.
 *
 * Nền đen tuyệt đối ở riêng màn hình này — khác với nền xám của phần còn lại.
 * Lý do: khi ảnh chiếm gần hết khung nhìn, không còn giao diện nào để đọc tương
 * quan nữa, nên đen cho ảnh nổi nhất. Ở các màn hình khác thì ngược lại, đen
 * tuyệt đối làm vùng tối của ảnh biến mất vào nền.
 */
export function ClientReviewScreen({
  shot,
  presets,
  presetId,
  onExit,
}: {
  shot: ShotView;
  presets: PresetView[];
  presetId: string;
  onExit: () => void;
}) {
  const { width, height } = useWindowDimensions();
  const preset = presets.find(p => p.id === presetId) ?? presets[0];

  return (
    <Pressable style={s.wrap} onPress={onExit}>
      <GradedImage
        uri={shot.uri}
        preset={preset}
        amount={1}
        width={width}
        height={Math.min(height * 0.82, width * 0.75)}
        showApproximationBadge={false}
      />
      {/* Gợi ý thoát rất mờ: đủ để nhiếp ảnh gia thấy, không đủ để khách chú ý. */}
      <Text style={s.hint}>chạm để thoát</Text>
    </Pressable>
  );
}

const s = StyleSheet.create({
  wrap: { flex: 1, backgroundColor: '#000', alignItems: 'center', justifyContent: 'center' },
  hint: {
    ...typography.caption,
    color: 'rgba(255,255,255,0.18)',
    position: 'absolute',
    bottom: spacing.xl,
  },
});
