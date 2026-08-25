import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import { Button, Divider, Field } from '../ui/components';
import { colors, radius, spacing, typography } from '../ui/theme';

/**
 * Màn hình đăng nhập.
 *
 * THỨ TỰ CÁC NÚT KHÔNG PHẢI LỰA CHỌN THẨM MỸ. App Store guideline 4.8 buộc
 * Sign in with Apple phải là lựa chọn TƯƠNG ĐƯƠNG khi đã có đăng nhập bên thứ ba
 * khác. Đặt Apple sau Google, hoặc làm nó nhỏ hơn, mờ hơn, là rủi ro bị từ chối
 * duyệt — và lý do từ chối sẽ đến sau khi đã nộp bản build, tốn cả tuần.
 *
 * Xem docs/adr/0002-auth-and-storage.md.
 */
export function SignInScreen({ onSignedIn }: { onSignedIn: () => void }) {
  const [mode, setMode] = useState<'choose' | 'email'>('choose');
  const [isSignUp, setIsSignUp] = useState(false);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');

  return (
    <ScrollView contentContainerStyle={s.wrap}>
      <View style={s.brand}>
        <Text style={s.logo}>◐</Text>
        <Text style={s.title}>Camera Picture</Text>
        <Text style={s.tagline}>Màu của bạn, ngay tại buổi chụp</Text>
      </View>

      {mode === 'choose' ? (
        <View style={s.stack}>
          <Button label="Đăng nhập với Apple" icon="" onPress={onSignedIn} />
          <Button label="Đăng nhập với Google" icon="G" variant="secondary" onPress={onSignedIn} />

          <View style={s.orRow}>
            <View style={s.orLine} />
            <Text style={s.orText}>hoặc</Text>
            <View style={s.orLine} />
          </View>

          <Button label="Dùng email" variant="ghost" onPress={() => setMode('email')} />
        </View>
      ) : (
        <View style={s.stack}>
          <Field label="Email">
            <TextInput
              value={email}
              onChangeText={setEmail}
              style={s.input}
              placeholder="ban@example.com"
              placeholderTextColor={colors.textFaint}
              autoCapitalize="none"
              keyboardType="email-address"
            />
          </Field>

          <Field
            label="Mật khẩu"
            hint={isSignUp ? 'Tối thiểu 12 ký tự. Độ dài quan trọng hơn ký tự đặc biệt.' : undefined}
          >
            <TextInput
              value={password}
              onChangeText={setPassword}
              style={s.input}
              secureTextEntry
              placeholder="••••••••••••"
              placeholderTextColor={colors.textFaint}
            />
          </Field>

          <Button label={isSignUp ? 'Tạo tài khoản' : 'Đăng nhập'} onPress={onSignedIn} />

          <Pressable onPress={() => setIsSignUp(v => !v)} style={s.switchRow}>
            <Text style={s.switchText}>
              {isSignUp ? 'Đã có tài khoản? Đăng nhập' : 'Chưa có tài khoản? Đăng ký'}
            </Text>
          </Pressable>

          <Divider />
          <Button label="Quay lại" variant="ghost" onPress={() => setMode('choose')} />
        </View>
      )}

      <Text style={s.legal}>
        Tiếp tục nghĩa là bạn đồng ý với Điều khoản sử dụng và Chính sách quyền riêng tư.
      </Text>
    </ScrollView>
  );
}

const s = StyleSheet.create({
  wrap: {
    flexGrow: 1,
    justifyContent: 'center',
    padding: spacing.xl,
    backgroundColor: colors.background,
  },
  brand: { alignItems: 'center', marginBottom: spacing.xxl },
  logo: { fontSize: 48, color: colors.accent, marginBottom: spacing.md },
  title: { ...typography.title, fontSize: 26 },
  tagline: { ...typography.caption, marginTop: spacing.xs },

  stack: { gap: spacing.md },

  orRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, marginVertical: spacing.xs },
  orLine: { flex: 1, height: 1, backgroundColor: colors.border },
  orText: { ...typography.caption, color: colors.textFaint },

  input: {
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: radius.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
    color: colors.text,
    fontSize: 15,
  },

  switchRow: { alignItems: 'center', paddingVertical: spacing.sm },
  switchText: { ...typography.caption, color: colors.accent },

  legal: {
    ...typography.caption,
    color: colors.textFaint,
    textAlign: 'center',
    marginTop: spacing.xxl,
    lineHeight: 17,
  },
});
