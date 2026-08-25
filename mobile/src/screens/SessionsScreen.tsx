import { FlatList, Pressable, StyleSheet, Text, View } from 'react-native';
import { Button, Pill } from '../ui/components';
import { colors, radius, spacing, typography } from '../ui/theme';
import { EmptyState } from '../ui/components';
import type { SessionView } from './types';

/**
 * Danh sách buổi chụp.
 *
 * Buổi đang tether được đẩy lên đầu và đánh dấu rõ: khi đang chụp, đó là thứ
 * duy nhất người dùng quan tâm, và họ mở app bằng một tay trong lúc tay kia cầm
 * máy ảnh.
 */
export function SessionsScreen({
  sessions,
  loading,
  onOpenSession,
  onOpenSettings,
  onNewSession,
}: {
  sessions: SessionView[];
  loading?: boolean;
  onOpenSession: (id: string) => void;
  onOpenSettings: () => void;
  onNewSession?: () => void;
}) {

  return (
    <View style={s.wrap}>
      <View style={s.header}>
        <View>
          <Text style={s.title}>Buổi chụp</Text>
          <Text style={s.sub}>{loading ? 'đang tải…' : `${sessions.length} buổi`}</Text>
        </View>
        <Pressable onPress={onOpenSettings} style={s.iconBtn} accessibilityLabel="Cài đặt">
          <Text style={s.icon}>⚙</Text>
        </Pressable>
      </View>

      <FlatList
        data={sessions}
        keyExtractor={item => item.id}
        contentContainerStyle={s.list}
        ItemSeparatorComponent={() => <View style={{ height: spacing.md }} />}
        renderItem={({ item }) => (
          <Pressable
            onPress={() => onOpenSession(item.id)}
            style={({ pressed }) => [s.row, item.live && s.rowLive, pressed && s.rowPressed]}
          >
            <View style={s.rowMain}>
              <View style={s.rowTop}>
                <Text style={s.rowTitle} numberOfLines={1}>
                  {item.name}
                </Text>
                {item.live ? <Pill label="ĐANG TETHER" tone="success" dot /> : null}
              </View>
              <Text style={s.rowMeta}>
                {item.client} · {item.date} · {item.shots.toLocaleString('vi-VN')} ảnh
              </Text>
            </View>
            <Text style={s.chevron}>›</Text>
          </Pressable>
        )}
        ListEmptyComponent={
          loading ? null : (
            <EmptyState
              icon="◐"
              title="Chưa có buổi chụp nào"
              body="Tạo buổi chụp rồi kết nối máy ảnh để ảnh bắt đầu chảy về."
            />
          )
        }
        ListFooterComponent={
          <Button
            label="Buổi chụp mới"
            icon="+"
            variant="secondary"
            onPress={onNewSession}
            style={{ marginTop: spacing.xl }}
          />
        }
      />
    </View>
  );
}

const s = StyleSheet.create({
  wrap: { flex: 1, backgroundColor: colors.background },
  header: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    justifyContent: 'space-between',
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.xl,
    paddingBottom: spacing.lg,
  },
  title: { ...typography.title },
  sub: { ...typography.caption, marginTop: 2 },
  iconBtn: { padding: spacing.sm },
  icon: { fontSize: 20, color: colors.textMuted },

  list: { paddingHorizontal: spacing.lg, paddingBottom: spacing.xxl },

  row: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.lg,
    padding: spacing.lg,
    gap: spacing.md,
  },
  rowLive: { borderColor: colors.success + '55' },
  rowPressed: { opacity: 0.75 },
  rowMain: { flex: 1, gap: spacing.xs },
  rowTop: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  rowTitle: { ...typography.heading, flexShrink: 1 },
  rowMeta: { ...typography.caption },
  chevron: { fontSize: 22, color: colors.textFaint },
});
