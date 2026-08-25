import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { Button, Card, Notice, Pill, SectionLabel } from '../ui/components';
import { colors, radius, spacing, typography } from '../ui/theme';
import { formatBytes } from '../demo/fixtures';
import type { StorageCapability, StorageProvider, StorageUsage } from '../account/types';
import type { StorageOptionView } from './types';

const PROVIDER_LABEL: Record<StorageProvider, string> = {
  device: 'Chỉ trên máy',
  managed: 'Lưu trữ của Camera Picture',
  google_drive: 'Google Drive của bạn',
  icloud: 'iCloud của bạn',
};

const PROVIDER_SUB: Record<StorageProvider, string> = {
  device: 'Ảnh ở lại trên thẻ nhớ và điện thoại. Không tốn phí.',
  managed: 'Chúng tôi giữ ảnh. Cần mua dung lượng.',
  google_drive: 'Bạn dùng dung lượng Drive đã có. Không mua gì thêm.',
  icloud: 'Bạn dùng dung lượng iCloud đã có.',
};

const CAPABILITY_LABEL: Record<StorageCapability, string> = {
  serverSideRender: 'Kết xuất RAW trên máy chủ',
  enforcedQuota: 'Hiển thị dung lượng còn lại',
  durable: 'Không mất khi thu hồi quyền',
};

/**
 * Chọn nơi lưu ảnh.
 *
 * Màn hình này cố ý KHÔNG bán hàng. Nó trình bày Google Drive ngang hàng với
 * lưu trữ trả phí, kể cả khi lựa chọn đó không mang lại doanh thu — vì với
 * storefront ngoài Hoa Kỳ, bán dung lượng của mình còn mất 15-30% hoa hồng cho
 * Apple, nên khoảng chênh không lớn như tưởng. Xem ADR 0002.
 *
 * Quan trọng hơn: mỗi lựa chọn hiển thị KHẢ NĂNG của nó và CẢNH BÁO mất dữ liệu
 * ngay tại chỗ, trước khi người dùng chọn. Với Drive và iCloud, người dùng hết
 * dung lượng hay thu hồi quyền là ảnh biến mất và app không làm gì được. Giấu
 * điều đó trong điều khoản sử dụng là khác biệt giữa một sản phẩm trung thực và
 * một vụ mất ảnh cưới.
 */
export function StorageScreen({
  options,
  selected,
  usage,
  onSelect,
  onBuyStorage,
  onLinkDrive,
  onBack,
}: {
  options: StorageOptionView[];
  selected: StorageProvider;
  /** null khi chưa đọc được, hoặc khi nhà cung cấp không báo hạn mức. */
  usage: StorageUsage | null;
  onSelect: (p: StorageProvider) => void;
  onBuyStorage?: () => void;
  onLinkDrive?: () => void;
  onBack: () => void;
}) {
  const pct = usage && usage.limitBytes > 0 ? Math.min(1, usage.usedBytes / usage.limitBytes) : 0;
  const nearlyFull = pct > 0.85;

  return (
    <View style={s.wrap}>
      <View style={s.header}>
        <Pressable onPress={onBack} style={s.backBtn} accessibilityLabel="Quay lại">
          <Text style={s.back}>‹</Text>
        </Pressable>
        <Text style={s.title}>Nơi lưu ảnh</Text>
      </View>

      <ScrollView contentContainerStyle={s.body}>
        {selected === 'managed' && usage ? (
          <Card>
            <View style={s.quotaHead}>
              <SectionLabel>DUNG LƯỢNG</SectionLabel>
              {nearlyFull ? <Pill label="SẮP ĐẦY" tone="warning" dot /> : null}
            </View>
            <Text style={s.quotaNum}>
              {formatBytes(usage.usedBytes)}
              <Text style={s.quotaOf}> / {formatBytes(usage.limitBytes)}</Text>
            </Text>
            <View style={s.bar}>
              <View
                style={[
                  s.barFill,
                  { width: `${pct * 100}%`, backgroundColor: nearlyFull ? colors.warning : colors.accent },
                ]}
              />
            </View>
            <Button label="Mua thêm dung lượng" onPress={onBuyStorage} style={{ marginTop: spacing.lg }} />
          </Card>
        ) : null}

        <SectionLabel>LỰA CHỌN</SectionLabel>

        {options.map(opt => {
          const active = opt.provider === selected;
          return (
            <Pressable
              key={opt.provider}
              onPress={() => onSelect(opt.provider)}
              style={({ pressed }) => [s.opt, active && s.optActive, pressed && { opacity: 0.8 }]}
            >
              <View style={s.optHead}>
                <View style={[s.radio, active && s.radioOn]}>
                  {active ? <View style={s.radioDot} /> : null}
                </View>
                <View style={{ flex: 1 }}>
                  <Text style={s.optTitle}>{PROVIDER_LABEL[opt.provider]}</Text>
                  <Text style={s.optSub}>{PROVIDER_SUB[opt.provider]}</Text>
                </View>
              </View>

              {opt.capabilities.length > 0 ? (
                <View style={s.caps}>
                  {opt.capabilities.map(c => (
                    <Pill key={c} label={CAPABILITY_LABEL[c as StorageCapability] ?? c} tone="success" />
                  ))}
                </View>
              ) : null}

              {/* Khác biệt tính năng phải hiện TRƯỚC khi chọn, không phải để
                  người dùng phát hiện lúc bấm nút xuất file. */}
              {!opt.capabilities.includes('serverSideRender') && opt.provider !== 'device' ? (
                <Pill label="Không kết xuất RAW trên máy chủ" tone="warning" />
              ) : null}

              {opt.warning ? <Notice tone="warning">{opt.warning}</Notice> : null}

              {opt.provider === 'google_drive' && active ? (
                <Button label="Liên kết Google Drive" variant="secondary" icon="G" onPress={onLinkDrive} />
              ) : null}
            </Pressable>
          );
        })}

        <Notice tone="info">
          Đổi nơi lưu KHÔNG tự chuyển ảnh cũ. Ảnh đã lưu ở nơi trước vẫn nằm nguyên đó.
        </Notice>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  wrap: { flex: 1, backgroundColor: colors.background },
  header: { flexDirection: 'row', alignItems: 'center', paddingTop: spacing.lg, paddingBottom: spacing.md },
  backBtn: { paddingHorizontal: spacing.md, paddingVertical: spacing.xs },
  back: { fontSize: 30, color: colors.textMuted, lineHeight: 32 },
  title: { ...typography.title, fontSize: 20 },

  body: { padding: spacing.lg, gap: spacing.lg, paddingBottom: spacing.xxl },

  quotaHead: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  quotaNum: { ...typography.title, fontSize: 26, marginBottom: spacing.md },
  quotaOf: { ...typography.body, color: colors.textMuted, fontSize: 16 },
  bar: { height: 6, borderRadius: radius.pill, backgroundColor: colors.surfaceRaised, overflow: 'hidden' },
  barFill: { height: 6 },

  opt: {
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    padding: spacing.lg,
    gap: spacing.md,
  },
  optActive: { borderColor: colors.accent },
  optHead: { flexDirection: 'row', alignItems: 'flex-start', gap: spacing.md },
  optTitle: { ...typography.heading, fontSize: 15 },
  optSub: { ...typography.caption, marginTop: 2, lineHeight: 17 },

  radio: {
    width: 20,
    height: 20,
    borderRadius: 10,
    borderWidth: 2,
    borderColor: colors.border,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 1,
  },
  radioOn: { borderColor: colors.accent },
  radioDot: { width: 9, height: 9, borderRadius: 5, backgroundColor: colors.accent },

  caps: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.xs },
});
