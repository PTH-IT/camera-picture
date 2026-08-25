import type { ReactNode } from 'react';
import {
  ActivityIndicator,
  Pressable,
  StyleSheet,
  Text,
  View,
  type StyleProp,
  type ViewStyle,
} from 'react-native';
import { colors, radius, spacing, typography, HIT_SIZE } from './theme';

export function Button({
  label,
  onPress,
  variant = 'primary',
  icon,
  disabled,
  loading,
  style,
}: {
  label: string;
  onPress?: () => void;
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  icon?: string;
  disabled?: boolean;
  loading?: boolean;
  style?: StyleProp<ViewStyle>;
}) {
  const isDisabled = disabled || loading;
  return (
    <Pressable
      onPress={isDisabled ? undefined : onPress}
      accessibilityRole="button"
      accessibilityState={{ disabled: isDisabled }}
      style={({ pressed }) => [
        s.btn,
        variant === 'primary' && s.btnPrimary,
        variant === 'secondary' && s.btnSecondary,
        variant === 'ghost' && s.btnGhost,
        variant === 'danger' && s.btnDanger,
        pressed && !isDisabled && s.btnPressed,
        isDisabled && s.btnDisabled,
        style,
      ]}
    >
      {loading ? (
        <ActivityIndicator color={variant === 'primary' ? colors.accentText : colors.text} />
      ) : (
        <>
          {icon ? <Text style={s.btnIcon}>{icon}</Text> : null}
          <Text
            style={[
              s.btnLabel,
              variant === 'primary' && { color: colors.accentText },
              variant === 'danger' && { color: colors.danger },
            ]}
          >
            {label}
          </Text>
        </>
      )}
    </Pressable>
  );
}

export function Card({ children, style }: { children: ReactNode; style?: StyleProp<ViewStyle> }) {
  return <View style={[s.card, style]}>{children}</View>;
}

/**
 * Nhãn trạng thái. `tone` mang ý nghĩa ngữ nghĩa chứ không chỉ là màu — dùng
 * `warning` cho thứ người dùng cần biết nhưng không phải lỗi, ví dụ "ảnh vẫn ở
 * trên thẻ".
 */
export function Pill({
  label,
  tone = 'neutral',
  dot,
}: {
  label: string;
  tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'accent';
  dot?: boolean;
}) {
  const toneColor = {
    neutral: colors.textMuted,
    success: colors.success,
    warning: colors.warning,
    danger: colors.danger,
    accent: colors.accent,
  }[tone];

  return (
    <View style={[s.pill, { borderColor: toneColor + '55' }]}>
      {dot ? <View style={[s.dot, { backgroundColor: toneColor }]} /> : null}
      <Text style={[s.pillText, { color: toneColor }]}>{label}</Text>
    </View>
  );
}

export function SectionLabel({ children }: { children: ReactNode }) {
  return <Text style={s.sectionLabel}>{children}</Text>;
}

/**
 * Thông báo cảnh báo hiển thị NGAY tại chỗ người dùng ra quyết định.
 *
 * Dùng cho các cảnh báo mất dữ liệu ở màn hình chọn nơi lưu trữ. Đây là yêu cầu
 * sản phẩm, không phải trang trí: với Drive và iCloud, người dùng hết dung lượng
 * hay thu hồi quyền là ảnh biến mất và app không làm gì được. Giấu điều đó trong
 * điều khoản sử dụng là khác biệt giữa sản phẩm trung thực và một vụ mất ảnh cưới.
 */
export function Notice({
  tone = 'warning',
  children,
}: {
  tone?: 'warning' | 'danger' | 'info';
  children: ReactNode;
}) {
  const toneColor = { warning: colors.warning, danger: colors.danger, info: colors.accent }[tone];
  return (
    <View style={[s.notice, { borderLeftColor: toneColor }]}>
      <Text style={s.noticeText}>{children}</Text>
    </View>
  );
}

/** Ô nhập liệu. Nhãn luôn hiện, không dùng placeholder thay nhãn — placeholder
 *  biến mất khi gõ, và người dùng quên mất ô đó là gì. */
export function Field({
  label,
  children,
  hint,
}: {
  label: string;
  children: ReactNode;
  hint?: string;
}) {
  return (
    <View style={s.field}>
      <Text style={s.fieldLabel}>{label}</Text>
      {children}
      {hint ? <Text style={s.fieldHint}>{hint}</Text> : null}
    </View>
  );
}

export function Divider() {
  return <View style={s.divider} />;
}

export function EmptyState({
  icon,
  title,
  body,
  action,
}: {
  icon: string;
  title: string;
  body: string;
  action?: ReactNode;
}) {
  return (
    <View style={s.empty}>
      <Text style={s.emptyIcon}>{icon}</Text>
      <Text style={s.emptyTitle}>{title}</Text>
      <Text style={s.emptyBody}>{body}</Text>
      {action ? <View style={{ marginTop: spacing.lg }}>{action}</View> : null}
    </View>
  );
}

const s = StyleSheet.create({
  btn: {
    minHeight: HIT_SIZE,
    paddingHorizontal: spacing.lg,
    borderRadius: radius.md,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.sm,
  },
  btnPrimary: { backgroundColor: colors.accent },
  btnSecondary: { backgroundColor: colors.surfaceRaised, borderWidth: 1, borderColor: colors.border },
  btnGhost: { backgroundColor: 'transparent' },
  btnDanger: { backgroundColor: 'transparent', borderWidth: 1, borderColor: colors.danger + '66' },
  btnPressed: { opacity: 0.7 },
  btnDisabled: { opacity: 0.4 },
  btnIcon: { fontSize: 16 },
  btnLabel: { ...typography.body, fontWeight: '600' },

  card: {
    backgroundColor: colors.surface,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.border,
    padding: spacing.lg,
  },

  pill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    paddingHorizontal: spacing.sm,
    paddingVertical: 3,
    borderRadius: radius.pill,
    borderWidth: 1,
    alignSelf: 'flex-start',
  },
  pillText: { fontSize: 11, fontWeight: '600' },
  dot: { width: 6, height: 6, borderRadius: 3 },

  sectionLabel: {
    ...typography.label,
    textTransform: 'uppercase',
    letterSpacing: 0.6,
    marginBottom: spacing.sm,
  },

  notice: {
    backgroundColor: colors.surfaceRaised,
    borderLeftWidth: 3,
    borderRadius: radius.sm,
    padding: spacing.md,
  },
  noticeText: { ...typography.caption, lineHeight: 18, color: colors.textMuted },

  field: { gap: spacing.sm },
  fieldLabel: { ...typography.label },
  fieldHint: { ...typography.caption, color: colors.textFaint },

  divider: { height: 1, backgroundColor: colors.border },

  empty: { alignItems: 'center', paddingVertical: spacing.xxl * 2, paddingHorizontal: spacing.xl },
  emptyIcon: { fontSize: 40, marginBottom: spacing.md },
  emptyTitle: { ...typography.heading, marginBottom: spacing.xs },
  emptyBody: { ...typography.caption, textAlign: 'center', lineHeight: 19, maxWidth: 280 },
});
