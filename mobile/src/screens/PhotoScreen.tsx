import { useState } from 'react';
import {
  LayoutChangeEvent,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { GradedImage } from '../color/GradedImage';
import { Slider } from '../ui/Slider';
import { Pill } from '../ui/components';
import { colors, radius, spacing, typography, HIT_SIZE } from '../ui/theme';
import { demoPresets, type DemoShot } from '../demo/fixtures';

/**
 * Màn hình xem và chỉnh một ảnh. Đây là nơi giá trị của sản phẩm nằm.
 *
 * Ba quyết định bố cục, đều có lý do:
 *
 * 1. Ảnh chiếm phần lớn màn hình và nằm trên nền trung tính tối. Mọi thứ khác là
 *    chrome, và chrome cạnh tranh với việc đánh giá màu.
 *
 * 2. Dải preset nằm NGANG ngay dưới ảnh, không giấu trong menu. So sánh các look
 *    cạnh nhau là thao tác chính ở màn hình này, không phải thao tác phụ.
 *
 * 3. Nút cull (sao, cờ, loại) ở cạnh dưới, trong tầm ngón cái. Người dùng lướt
 *    hàng trăm ảnh bằng một tay trong lúc tay kia cầm máy.
 */
export function PhotoScreen({
  shot,
  presetId,
  onPresetChange,
  onBack,
  onClientMode,
}: {
  shot: DemoShot;
  presetId: string;
  onPresetChange: (id: string) => void;
  onBack: () => void;
  onClientMode: () => void;
}) {
  // Đo vùng ảnh thay vì tính từ chiều rộng cửa sổ.
  //
  // Cách cũ (chiều cao = 0.68 × chiều rộng) cho ra một tấm ảnh nhỏ giữa màn hình
  // với khoảng trống chết bên dưới trên máy màn hình dài. Ảnh LÀ nội dung của màn
  // hình này, nên nó phải lấy hết phần còn lại — và phần còn lại chỉ biết được
  // sau khi đo, vì chiều cao khối điều khiển thay đổi theo cỡ chữ của hệ thống.
  const [imgBox, setImgBox] = useState({ width: 0, height: 0 });
  const onImgLayout = (e: LayoutChangeEvent) => {
    const { width, height } = e.nativeEvent.layout;
    setImgBox({ width: Math.round(width), height: Math.round(height) });
  };

  const [amount, setAmount] = useState(0.85);
  const [rating, setRating] = useState(shot.rating);
  const [flagged, setFlagged] = useState(shot.flagged);
  const [rejected, setRejected] = useState(shot.rejected);

  const preset = demoPresets.find(p => p.id === presetId) ?? demoPresets[0]!;

  return (
    <View style={s.wrap}>
      <View style={s.header}>
        <Pressable onPress={onBack} style={s.iconBtn} accessibilityLabel="Quay lại">
          <Text style={s.back}>‹</Text>
        </Pressable>
        <View style={s.headerMain}>
          <Text style={s.filename}>{shot.filename}</Text>
          <Text style={s.exif}>
            {shot.focal} · {shot.aperture} · {shot.shutter} · ISO {shot.iso}
          </Text>
        </View>
        <Pressable onPress={onClientMode} style={s.iconBtn} accessibilityLabel="Chế độ khách xem">
          <Text style={s.icon}>⛶</Text>
        </Pressable>
      </View>

      <View style={s.imgBox} onLayout={onImgLayout}>
        {imgBox.width > 0 ? (
          <GradedImage
            uri={shot.uri}
            preset={preset}
            amount={amount}
            width={imgBox.width}
            height={imgBox.height}
          />
        ) : null}

        {/* Nhãn trạng thái nằm ĐÈ lên ảnh thay vì chiếm một hàng riêng: mỗi hàng
            chrome là một phần chiều cao lấy đi của ảnh. */}
        <View style={s.badges}>
        {/* Trạng thái lưu trữ hiện ngay trên ảnh. "RAW trên thẻ" là trạng thái
            BÌNH THƯỜNG với phần lớn ảnh — nói rõ để người dùng không tưởng là
            đồng bộ hỏng. Xem docs/adr/0001-capture-strategy.md. */}
          <Pill
            label={shot.originalUploaded ? 'RAW đã lên máy chủ' : 'RAW trên thẻ'}
            tone={shot.originalUploaded ? 'success' : 'warning'}
            dot
          />
          <Pill label={shot.time} />
        </View>
      </View>

      <View style={s.controls}>
        <Text style={s.sectionLabel}>PRESET MÀU</Text>
        {/* Bọc trong View có overflow hidden: ScrollView ngang mà không bị kẹp
            sẽ kéo giãn chiều rộng của cha, và mọi thứ căn theo mép phải (nút
            chế độ khách, nút cull) bị đẩy ra ngoài khung. */}
        <View style={s.stripClip}>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={s.strip}>
          {demoPresets.map(p => {
            const active = p.id === preset.id;
            return (
              <Pressable key={p.id} onPress={() => onPresetChange(p.id)} style={s.chipWrap}>
                <View style={[s.chipImg, active && s.chipImgActive]}>
                  <GradedImage
                    uri={shot.uri}
                    preset={p}
                    amount={1}
                    width={62}
                    height={62}
                    showApproximationBadge={false}
                  />
                </View>
                <Text style={[s.chipLabel, active && s.chipLabelActive]} numberOfLines={1}>
                  {p.name}
                </Text>
              </Pressable>
            );
          })}
        </ScrollView>
        </View>

        <Slider label="Cường độ" value={amount} onChange={setAmount} />

        <View style={s.cullRow}>
          <View style={s.stars}>
            {[1, 2, 3, 4, 5].map(n => (
              <Pressable
                key={n}
                onPress={() => setRating(rating === n ? 0 : n)}
                style={s.star}
                accessibilityLabel={`${n} sao`}
              >
                <Text style={[s.starText, n <= rating && s.starOn]}>★</Text>
              </Pressable>
            ))}
          </View>

          <View style={s.cullBtns}>
            <Pressable
              onPress={() => {
                setFlagged(v => !v);
                if (!flagged) setRejected(false);
              }}
              style={[s.cullBtn, flagged && { borderColor: colors.flagged, backgroundColor: colors.flagged + '1a' }]}
            >
              <Text style={[s.cullIcon, flagged && { color: colors.flagged }]}>⚑</Text>
            </Pressable>
            <Pressable
              onPress={() => {
                setRejected(v => !v);
                if (!rejected) setFlagged(false);
              }}
              style={[s.cullBtn, rejected && { borderColor: colors.rejected, backgroundColor: colors.rejected + '1a' }]}
            >
              <Text style={[s.cullIcon, rejected && { color: colors.rejected }]}>✕</Text>
            </Pressable>
          </View>
        </View>
      </View>
    </View>
  );
}

const s = StyleSheet.create({
  wrap: { flex: 1, backgroundColor: colors.canvas, overflow: 'hidden' },
  header: { flexDirection: 'row', alignItems: 'center', paddingTop: spacing.lg, paddingBottom: spacing.md },
  iconBtn: { width: HIT_SIZE, height: HIT_SIZE, alignItems: 'center', justifyContent: 'center' },
  back: { fontSize: 30, color: colors.textMuted, lineHeight: 32 },
  icon: { fontSize: 18, color: colors.textMuted },
  headerMain: { flex: 1 },
  filename: { ...typography.heading, fontSize: 15 },
  exif: { ...typography.mono, marginTop: 2 },

  imgBox: { flex: 1, backgroundColor: colors.canvas },
  badges: {
    position: 'absolute',
    left: spacing.lg,
    bottom: spacing.md,
    flexDirection: 'row',
    gap: spacing.sm,
  },

  controls: {
    backgroundColor: colors.background,
    borderTopLeftRadius: radius.lg,
    borderTopRightRadius: radius.lg,
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.lg,
    gap: spacing.md,
  },
  sectionLabel: { ...typography.label, letterSpacing: 0.6 },

  stripClip: { overflow: 'hidden' },
  strip: { gap: spacing.md, paddingRight: spacing.lg },
  chipWrap: { alignItems: 'center', gap: spacing.xs, width: 66 },
  chipImg: {
    borderRadius: radius.sm,
    overflow: 'hidden',
    borderWidth: 2,
    borderColor: 'transparent',
  },
  chipImgActive: { borderColor: colors.accent },
  chipLabel: { ...typography.caption, fontSize: 11, color: colors.textFaint },
  chipLabelActive: { color: colors.text },

  cullRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingTop: spacing.sm,
  },
  stars: { flexDirection: 'row' },
  star: { width: 38, height: HIT_SIZE, alignItems: 'center', justifyContent: 'center' },
  starText: { fontSize: 22, color: colors.textFaint },
  starOn: { color: colors.warning },

  cullBtns: { flexDirection: 'row', gap: spacing.sm },
  cullBtn: {
    width: HIT_SIZE,
    height: HIT_SIZE,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.border,
    alignItems: 'center',
    justifyContent: 'center',
  },
  cullIcon: { fontSize: 18, color: colors.textMuted },
});
