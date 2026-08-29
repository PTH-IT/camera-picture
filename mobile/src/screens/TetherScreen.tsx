import { FlatList, Pressable, StyleSheet, Text, View, useWindowDimensions } from 'react-native';
import { GradedImage } from '../color/GradedImage';
import { EmptyState, Pill } from '../ui/components';
import { colors, radius, spacing, typography } from '../ui/theme';
import type { CameraView, PresetView, ShotView } from './types';

const GAP = 2;

/**
 * Màn hình tether trực tiếp.
 *
 * Đây là màn hình người dùng nhìn nhiều nhất trong lúc chụp, nên nó được thiết
 * kế cho việc LƯỚT NHANH chứ không phải xem kỹ: ảnh mới nhất luôn ở trên cùng,
 * lưới dày, tối thiểu chữ.
 *
 * Ảnh hiển thị là JPEG preview nhúng trong file RAW, không phải bản decode RAW —
 * xem docs/adr/0001-capture-strategy.md. Preview có sẵn trong file nên hiện được
 * dưới một giây sau khi bấm máy, còn RAW ở lại trên thẻ.
 */
export function TetherScreen({
  title,
  shots,
  camera,
  previewNeedsFullDownload = false,
  presets,
  presetId,
  onOpenShot,
  onBack,
}: {
  title: string;
  shots: ShotView[];
  /** null khi chưa kết nối máy ảnh nào. */
  camera: CameraView | null;
  /**
   * Máy ảnh này bắt buộc tải cả file RAW mới xem được ảnh.
   *
   * Phải nói ra, không được im lặng chịu đựng: 2000 tấm × 55MB thì lưới ảnh sẽ
   * không bao giờ đầy, và người dùng cần biết đó là giới hạn của máy ảnh chứ
   * không phải app hỏng.
   */
  previewNeedsFullDownload?: boolean;
  presets: PresetView[];
  presetId: string;
  onOpenShot: (shot: ShotView) => void;
  onBack: () => void;
}) {
  const { width } = useWindowDimensions();
  const cols = width > 700 ? 4 : 3;
  const cell = Math.floor((width - GAP * (cols - 1)) / cols);

  const preset = presets.find(p => p.id === presetId) ?? presets[0];

  return (
    <View style={s.wrap}>
      <View style={s.header}>
        <Pressable onPress={onBack} style={s.backBtn} accessibilityLabel="Quay lại">
          <Text style={s.back}>‹</Text>
        </Pressable>
        <View style={s.headerMain}>
          <Text style={s.title} numberOfLines={1}>
            {title}
          </Text>
          <Text style={s.sub}>
            {shots.length} ảnh{preset ? ` · preset ${preset.name}` : ''}
          </Text>
        </View>
      </View>

      {/* Trạng thái kết nối luôn hiện. Khi tether rớt giữa buổi chụp, người dùng
          phải biết NGAY — mỗi giây không biết là một tấm ảnh có thể đã mất. */}
      {/* Cho xuống dòng thay vì đẩy bằng spacer: trên màn hình hẹp, spacer sẽ
          đẩy nhãn cuối ra ngoài khung và người dùng không bao giờ thấy nó. */}
      <View style={s.status}>
        {camera ? (
          <>
            <Pill label={camera.model} tone="success" dot />
            <Pill label={camera.transport === 'wifi' ? 'Wi-Fi' : 'USB-C'} />
            {/* Không đọc được pin thì không hiện ô nào. Hiện "Pin —%" chỉ làm
                người dùng tưởng máy ảnh sắp hết pin. */}
            {typeof camera.battery === 'number' ? (
              <Pill
                label={`Pin ${camera.battery}%`}
                tone={camera.battery < 20 ? 'warning' : 'neutral'}
              />
            ) : null}
            <Pill
              label={previewNeedsFullDownload ? 'Preview cần tải cả RAW' : 'RAW trên thẻ'}
              tone="warning"
            />
          </>
        ) : (
          <Pill label="Chưa kết nối máy ảnh" tone="warning" dot />
        )}
      </View>

      <FlatList
        data={shots}
        keyExtractor={item => item.id}
        numColumns={cols}
        key={cols}
        columnWrapperStyle={{ gap: GAP }}
        // rowGap chứ không phải gap: `gap` áp cho cả hai trục, cộng dồn với
        // columnWrapperStyle thành khoảng cách ngang gấp đôi và làm tràn cột cuối.
        contentContainerStyle={{ rowGap: GAP, paddingBottom: spacing.xxl }}
        ListEmptyComponent={
          <EmptyState
            icon="◌"
            title="Chưa có ảnh nào"
            body="Bấm máy để ảnh chảy về đây. Ảnh hiện ra dưới một giây; file RAW vẫn ở lại trên thẻ."
          />
        }
        renderItem={({ item, index }) => (
          <Pressable onPress={() => onOpenShot(item)} style={({ pressed }) => pressed && { opacity: 0.7 }}>
            <GradedImage
              uri={item.uri}
              preset={preset}
              amount={1}
              width={cell}
              height={cell}
              showApproximationBadge={false}
            />
            {/* Ảnh mới nhất được đánh dấu: trong lúc chụp liên tục, người dùng
                cần biết tấm nào vừa vào để không nhầm với tấm cũ. */}
            {index === 0 ? (
              <View style={s.newest}>
                <Text style={s.newestText}>MỚI</Text>
              </View>
            ) : null}
            {item.rejected ? <View style={[s.mark, { backgroundColor: colors.rejected }]} /> : null}
            {item.flagged && !item.rejected ? (
              <View style={[s.mark, { backgroundColor: colors.flagged }]} />
            ) : null}
          </Pressable>
        )}
      />
    </View>
  );
}

const s = StyleSheet.create({
  wrap: { flex: 1, backgroundColor: colors.canvas },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingTop: spacing.lg,
    paddingBottom: spacing.md,
    paddingRight: spacing.lg,
  },
  backBtn: { paddingHorizontal: spacing.md, paddingVertical: spacing.xs },
  back: { fontSize: 30, color: colors.textMuted, lineHeight: 32 },
  headerMain: { flex: 1 },
  title: { ...typography.heading },
  sub: { ...typography.caption, marginTop: 1 },

  status: {
    flexDirection: 'row',
    alignItems: 'center',
    flexWrap: 'wrap',
    gap: spacing.sm,
    paddingHorizontal: spacing.lg,
    paddingBottom: spacing.md,
  },

  newest: {
    position: 'absolute',
    top: 6,
    left: 6,
    backgroundColor: colors.accent,
    paddingHorizontal: 6,
    paddingVertical: 2,
    borderRadius: radius.sm,
  },
  newestText: { fontSize: 9, fontWeight: '700', color: colors.accentText, letterSpacing: 0.5 },

  mark: { position: 'absolute', top: 6, right: 6, width: 8, height: 8, borderRadius: 4 },
});
