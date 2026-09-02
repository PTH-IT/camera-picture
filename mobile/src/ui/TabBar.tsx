import { Pressable, StyleSheet, Text, View } from 'react-native';
import { colors, spacing, typography } from './theme';

export interface TabItem {
  key: string;
  label: string;
  icon: string;
}

/**
 * Thanh tab dưới màn hình.
 *
 * Chỉ hiện ở các màn hình GỐC (danh sách buổi chụp, chỉnh màu). Màn hình tether
 * và màn hình xem một tấm cố ý KHÔNG có nó: cả hai là nơi người dùng nhìn ảnh,
 * và một thanh chrome cao 56px ăn mất phần ảnh mà không đổi lại được gì — muốn
 * ra thì bấm nút quay lại đã có sẵn ở góc trên.
 *
 * Vùng an toàn dưới đáy do App xử lý một lần cho cả ứng dụng (SafeAreaView),
 * nên ở đây không cộng thêm lề — cộng hai lần sẽ đẩy thanh tab lên lơ lửng.
 */
export function TabBar({
  items,
  active,
  onSelect,
}: {
  items: TabItem[];
  active: string;
  onSelect: (key: string) => void;
}) {
  return (
    <View style={s.bar}>
      {items.map(item => {
        const on = item.key === active;
        return (
          <Pressable
            key={item.key}
            onPress={() => onSelect(item.key)}
            style={s.tab}
            accessibilityRole="tab"
            accessibilityState={{ selected: on }}
            accessibilityLabel={item.label}
          >
            <Text style={[s.icon, on && s.iconOn]}>{item.icon}</Text>
            <Text style={[s.label, on && s.labelOn]}>{item.label}</Text>
          </Pressable>
        );
      })}
    </View>
  );
}

const s = StyleSheet.create({
  bar: {
    flexDirection: 'row',
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.border,
    backgroundColor: colors.background,
    paddingTop: spacing.sm,
    paddingBottom: spacing.xs,
  },
  tab: { flex: 1, alignItems: 'center', gap: 2, paddingVertical: spacing.xs },
  icon: { fontSize: 20, color: colors.textFaint },
  iconOn: { color: colors.accent },
  label: { ...typography.caption, fontSize: 11, color: colors.textFaint },
  labelOn: { color: colors.accent },
});
