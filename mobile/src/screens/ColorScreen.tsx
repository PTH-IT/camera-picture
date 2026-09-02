import { useState } from 'react';
import {
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
  useWindowDimensions,
} from 'react-native';
import { GradedImage } from '../color/GradedImage';
import { ADJUSTMENT_KEYS, isNeutral, type ColorAdjustments } from '../color/adjustments';
import { Slider } from '../ui/Slider';
import { Button, EmptyState, Pill } from '../ui/components';
import { colors, radius, spacing, typography } from '../ui/theme';
import type { PresetView, SavedPresetView, ShotView } from './types';

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
  saved,
  savedId,
  onApplySaved,
  onSavePreset,
  onDeletePreset,
  onExportDevice,
  onExportStorage,
  exportBusy = false,
  exportNote = null,
  exportDisabledReason = null,
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

  /** Preset người dùng đã lưu trên máy chủ. */
  saved: SavedPresetView[];
  /** Preset đang áp, hoặc null nếu đang kéo tay. */
  savedId: string | null;
  onApplySaved: (id: string) => void;
  onSavePreset: (name: string) => void;
  onDeletePreset: (id: string) => void;

  /** Xuất ra máy (bảng chia sẻ của hệ điều hành). */
  onExportDevice: () => void;
  /** Xuất lên kho lưu trữ đã chọn, vào thư mục `da-chinh` của buổi chụp. */
  onExportStorage: () => void;
  exportBusy?: boolean;
  /** Kết quả lần xuất gần nhất, hoặc lý do không xuất được. */
  exportNote?: string | null;
  /** Có lý do thì hai nút bị khoá và lý do hiện ra thay vì để bấm vào không. */
  exportDisabledReason?: string | null;
}) {
  const [naming, setNaming] = useState(false);
  const [draftName, setDraftName] = useState('');
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

        <Text style={s.section}>Preset của bạn</Text>
        {saved.length === 0 && !naming ? (
          <Text style={s.hint}>
            Kéo màu tới khi ưng rồi lưu lại, để dùng cho những tấm còn lại của buổi chụp.
          </Text>
        ) : null}
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={s.presets}>
          {saved.map(p => (
            <Pressable key={p.id} onPress={() => onApplySaved(p.id)}>
              <Pill label={p.name} tone={p.id === savedId ? 'accent' : 'neutral'} />
            </Pressable>
          ))}
          {/* Nút xoá chỉ hiện cho preset ĐANG áp: một dấu × trên mọi ô làm hàng
              preset trông như một danh sách chờ dọn, và rất dễ bấm nhầm. */}
          {savedId ? (
            <Pressable
              onPress={() => onDeletePreset(savedId)}
              accessibilityLabel="Xoá preset đang chọn"
            >
              <Pill label="× Xoá" tone="danger" />
            </Pressable>
          ) : null}
        </ScrollView>

        {naming ? (
          <View style={s.nameRow}>
            <TextInput
              value={draftName}
              onChangeText={setDraftName}
              autoFocus
              placeholder="Tên preset"
              placeholderTextColor={colors.textFaint}
              style={s.nameInput}
            />
            <Button
              label="Lưu"
              disabled={draftName.trim() === ''}
              onPress={() => {
                onSavePreset(draftName.trim());
                setDraftName('');
                setNaming(false);
              }}
            />
          </View>
        ) : (
          <Button
            label="Lưu thành preset"
            variant="secondary"
            // Không có gì để lưu thì không mời người dùng lưu.
            disabled={isNeutral(adjustments)}
            onPress={() => setNaming(true)}
          />
        )}

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

        <Text style={s.section}>Xuất ảnh</Text>
        {/* Nói rõ đang xuất cái gì: ảnh nguồn là JPEG preview nhúng trong RAW,
            không phải bản kết xuất từ RAW. Người dùng phải biết trước khi gửi
            cho khách, không phải phát hiện khi in ra. */}
        <Text style={s.hint}>
          Xuất từ ảnh xem trước trong file RAW (~2MP) — đủ để gửi khách xem tại chỗ.
          Bản in phải kết xuất từ RAW, mà RAW vẫn nằm trên thẻ nhớ.
        </Text>

        <View style={s.exportRow}>
          <Button
            label="Lưu vào máy"
            variant="secondary"
            loading={exportBusy}
            disabled={exportBusy || exportDisabledReason !== null}
            onPress={onExportDevice}
            style={s.exportBtn}
          />
          <Button
            label="Lên kho lưu trữ"
            variant="secondary"
            loading={exportBusy}
            disabled={exportBusy || exportDisabledReason !== null}
            onPress={onExportStorage}
            style={s.exportBtn}
          />
        </View>
        {exportDisabledReason ?? exportNote ? (
          <Text style={s.hint}>{exportDisabledReason ?? exportNote}</Text>
        ) : null}
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
  hint: { ...typography.caption, lineHeight: 18 },

  exportRow: { flexDirection: 'row', gap: spacing.sm },
  exportBtn: { flex: 1 },

  nameRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  nameInput: {
    flex: 1,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    color: colors.text,
    fontSize: 15,
  },
});
