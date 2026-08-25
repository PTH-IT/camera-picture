import { useEffect, useRef, useState } from 'react';
import { FlatList, Pressable, StyleSheet, Text, View, useWindowDimensions } from 'react-native';
import { GradedImage } from '../color/GradedImage';
import { Pill } from '../ui/components';
import { colors, radius, spacing, typography } from '../ui/theme';
import { demoCamera, demoPresets, demoShots, type DemoShot } from '../demo/fixtures';

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
  onOpenShot,
  onBack,
  presetId,
}: {
  onOpenShot: (shot: DemoShot) => void;
  onBack: () => void;
  presetId: string;
}) {
  const { width } = useWindowDimensions();
  const cols = width > 700 ? 4 : 3;
  const cell = Math.floor((width - GAP * (cols - 1)) / cols);

  const preset = demoPresets.find(p => p.id === presetId) ?? demoPresets[0]!;

  // Mô phỏng ảnh chảy về trong lúc chụp. Trên máy thật, mỗi tấm tới từ một sự
  // kiện `itemCaptured` của CaptureSource.
  const all = useRef(demoShots(12)).current;
  const [visible, setVisible] = useState(9);
  useEffect(() => {
    if (visible >= all.length) return;
    const t = setTimeout(() => setVisible(v => Math.min(v + 1, all.length)), 1400);
    return () => clearTimeout(t);
  }, [visible, all.length]);

  const shots = all.slice(0, visible);

  return (
    <View style={s.wrap}>
      <View style={s.header}>
        <Pressable onPress={onBack} style={s.backBtn} accessibilityLabel="Quay lại">
          <Text style={s.back}>‹</Text>
        </Pressable>
        <View style={s.headerMain}>
          <Text style={s.title} numberOfLines={1}>
            Minh & Lan — Tiệc cưới
          </Text>
          <Text style={s.sub}>
            {shots.length} ảnh · preset {preset.name}
          </Text>
        </View>
      </View>

      {/* Trạng thái kết nối luôn hiện. Khi tether rớt giữa buổi chụp, người dùng
          phải biết NGAY — mỗi giây không biết là một tấm ảnh có thể đã mất. */}
      {/* Cho xuống dòng thay vì đẩy bằng spacer: trên màn hình hẹp, spacer sẽ
          đẩy nhãn cuối ra ngoài khung và người dùng không bao giờ thấy nó. */}
      <View style={s.status}>
        <Pill label={demoCamera.model} tone="success" dot />
        <Pill label={demoCamera.transport === 'wifi' ? 'Wi-Fi' : 'USB-C'} />
        <Pill label={`Pin ${demoCamera.battery}%`} tone={demoCamera.battery < 20 ? 'warning' : 'neutral'} />
        <Pill label="RAW trên thẻ" tone="warning" />
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
