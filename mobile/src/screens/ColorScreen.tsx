import { Pressable, ScrollView, StyleSheet, Text, View, useWindowDimensions } from 'react-native';
import { GradedImage } from '../color/GradedImage';
import { ADJUSTMENT_KEYS, isNeutral, type ColorAdjustments } from '../color/adjustments';
import { Slider } from '../ui/Slider';
import { EmptyState, Pill } from '../ui/components';
import { colors, radius, spacing, typography } from '../ui/theme';
import type { PresetView, ShotView } from './types';

/**
 * Bàn chỉnh màu.
 *
 * Khác màn hình xem ảnh ở mục đích: ở đó người dùng LƯỚT để chọn ảnh giữ hay
 * loại, ở đây họ DỪNG LẠI ở một tấm và kéo màu. Vì vậy bố cục ngược nhau — ảnh
 * cố định một cỡ vừa phải, còn phần lớn màn hình dành cho các thanh trượt.
 *
 * Ba quyết định:
 *
 * 1. Ảnh nằm TRÊN CÙNG và không cuộn đi mất. Kéo một thanh trượt mà không nhìn
 *    thấy ảnh là kéo mù.
 * 2. Dải ảnh nằm ngay dưới ảnh lớn. Đổi tấm là thao tác thường xuyên — so màu
 *    giữa hai tấm cạnh nhau là cách duy nhất để biết chỉnh đã đủ chưa.
 * 3. Preset và thanh trượt tách làm hai khối có tiêu đề. Preset là look dùng
 *    chung cho cả buổi; thanh trượt là hiệu chỉnh riêng của tấm này. Gộp chung
 *    một danh sách sẽ khiến người dùng tưởng chúng cùng loại.
 */
export function ColorScreen({
  shots,
  selectedId,
  onSelect,
  presets,
  presetId,
  onPresetChange,
  amount,
  onAmountChange,
  adjustments,
  onAdjust,
  onCommit,
  onReset,
}: {
  shots: ShotView[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  presets: PresetView[];
  presetId: string;
  onPresetChange: (id: string) => void;
  amount: number;
  onAmountChange: (v: number) => void;
  adjustments: ColorAdjustments;
  /** Bắn liên tục trong lúc kéo — chỉ để vẽ lại. */
  onAdjust: (next: ColorAdjustments) => void;
  /** Bắn khi nhả tay — chỗ duy nhất được phép chạm mạng. */
  onCommit: () => void;
  onReset: () => void;
}) {
  const { width } = useWindowDimensions();
  const preview = Math.round(width * 0.75);

  const preset = presets.find(p => p.id === presetId) ?? presets[0];
  const shot = shots.find(x => x.id === selectedId) ?? shots[0] ?? null;

  if (!shot) {
    return (
      <View style={s.wrap}>
        <View style={s.header}>
          <Text style={s.title}>Chỉnh màu</Text>
        </View>
        <EmptyState
          icon="◑"
          title="Chưa có ảnh để chỉnh"
          body="Mở một buổi chụp và kết nối máy ảnh. Ảnh về tới đâu, chỉnh được tới đó."
        />
      </View>
    );
  }

  const set = (key: keyof ColorAdjustments, v: number) => onAdjust({ ...adjustments, [key]: v });

  return (
    <View style={s.wrap}>
      <View style={s.header}>
        <View style={s.headerMain}>
          <Text style={s.title}>Chỉnh màu</Text>
          <Text style={s.sub} numberOfLines={1}>
            {shot.filename}
          </Text>
        </View>
        {/* Chỉ hiện khi có gì để đặt lại: một nút luôn bật nhưng thường không
            làm gì là nút người dùng học cách bỏ qua. */}
        {!isNeutral(adjustments) ? (
          <Pressable onPress={onReset} style={s.reset} accessibilityLabel="Đặt lại chỉnh màu">
            <Text style={s.resetText}>Đặt lại</Text>
          </Pressable>
        ) : null}
      </View>

      <GradedImage
        uri={shot.uri}
        preset={preset}
        amount={amount}
        adjustments={adjustments}
        width={width}
        height={preview}
        showApproximationBadge
      />

      <ScrollView contentContainerStyle={s.body}>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={s.strip}>
          {shots.map(item => (
            <Pressable key={item.id} onPress={() => onSelect(item.id)}>
              <GradedImage
                uri={item.uri}
                preset={preset}
                amount={amount}
                // Ảnh trong dải KHÔNG áp phần chỉnh tay: nó thuộc về riêng tấm
                // đang mở, còn dải này để so sánh giữa các tấm.
                width={56}
                height={56}
                showApproximationBadge={false}
                style={[s.thumb, item.id === shot.id && s.thumbOn]}
              />
            </Pressable>
          ))}
        </ScrollView>

        <Text style={s.section}>Bảng màu</Text>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={s.presets}>
          {presets.map(p => (
            <Pressable key={p.id} onPress={() => onPresetChange(p.id)}>
              <Pill label={p.name} tone={p.id === preset?.id ? 'accent' : 'neutral'} />
            </Pressable>
          ))}
        </ScrollView>

        <Slider
          label="Mức preset"
          value={amount}
          onChange={onAmountChange}
          onCommit={onCommit}
        />

        <Text style={s.section}>Chỉnh tay</Text>
        {ADJUSTMENT_KEYS.map(key => (
          <Slider
            key={key}
            label={LABELS[key]}
            bipolar
            // Thanh trượt làm việc trong [0,1]; chỉnh màu nằm trong [-1,1]. Quy
            // đổi ở đây thay vì trong Slider: một thanh trượt biết về đơn vị của
            // thứ nó điều khiển là một thanh trượt chỉ dùng được cho đúng thứ đó.
            value={(adjustments[key] + 1) / 2}
            onChange={v => set(key, v * 2 - 1)}
            onCommit={onCommit}
            format={v => formatSigned(v * 2 - 1, key)}
          />
        ))}
      </ScrollView>
    </View>
  );
}

const LABELS: Record<keyof ColorAdjustments, string> = {
  exposure: 'Phơi sáng',
  contrast: 'Tương phản',
  saturation: 'Bão hoà',
  temperature: 'Nhiệt độ',
  tint: 'Sắc',
  highlights: 'Vùng sáng',
  shadows: 'Vùng tối',
};

/**
 * Phơi sáng hiển thị theo KHẨU vì đó là đơn vị nhiếp ảnh gia nghĩ trong đầu;
 * phần còn lại là phần trăm vì chúng không có đơn vị vật lý nào cả.
 */
function formatSigned(v: number, key: keyof ColorAdjustments): string {
  if (Math.abs(v) < 0.005) return '0';
  if (key === 'exposure') return `${v > 0 ? '+' : ''}${(v * 2).toFixed(2)} EV`;
  return `${v > 0 ? '+' : ''}${Math.round(v * 100)}`;
}

const s = StyleSheet.create({
  wrap: { flex: 1, backgroundColor: colors.canvas },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.lg,
    paddingBottom: spacing.md,
    gap: spacing.md,
  },
  headerMain: { flex: 1 },
  title: { ...typography.heading },
  sub: { ...typography.caption, marginTop: 1 },
  reset: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: radius.pill,
    borderWidth: 1,
    borderColor: colors.border,
  },
  resetText: { ...typography.caption, color: colors.text },

  body: { padding: spacing.lg, gap: spacing.md, paddingBottom: spacing.xxl },
  strip: { gap: spacing.xs, paddingBottom: spacing.xs },
  thumb: { borderRadius: radius.sm, borderWidth: 2, borderColor: 'transparent' },
  thumbOn: { borderColor: colors.accent },

  section: { ...typography.label, marginTop: spacing.sm },
  presets: { gap: spacing.xs, paddingBottom: spacing.xs },
});
