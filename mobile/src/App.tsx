import { useState } from 'react';
import { StatusBar, StyleSheet, View } from 'react-native';
import { SignInScreen } from './screens/SignInScreen';
import { SessionsScreen } from './screens/SessionsScreen';
import { TetherScreen } from './screens/TetherScreen';
import { PhotoScreen } from './screens/PhotoScreen';
import { StorageScreen } from './screens/StorageScreen';
import { ClientReviewScreen } from './screens/ClientReviewScreen';
import { colors } from './ui/theme';
import type { DemoShot } from './demo/fixtures';

type Route =
  | { name: 'signin' }
  | { name: 'sessions' }
  | { name: 'tether' }
  | { name: 'photo'; shot: DemoShot }
  | { name: 'client'; shot: DemoShot }
  | { name: 'storage' };

/**
 * Vỏ ứng dụng và điều hướng.
 *
 * Cố ý dùng một máy trạng thái đơn giản thay vì react-navigation. Ứng dụng có
 * năm màn hình và luồng tuyến tính; thêm một thư viện điều hướng đầy đủ vào đây
 * là mang theo native module, cấu hình cho từng nền tảng, và mất khả năng xem
 * trước trên trình duyệt — đổi lấy những tính năng chưa cần đến.
 *
 * Khi luồng phức tạp lên (deep link, tab, quay lại theo lịch sử), hãy thay bằng
 * react-navigation. Đừng cố kéo dài cái này.
 */
export function App() {
  const [route, setRoute] = useState<Route>({ name: 'signin' });
  // Preset chọn ở màn hình ảnh phải giữ nguyên khi quay lại lưới: nhiếp ảnh gia
  // chọn một look cho cả buổi chụp, không phải cho từng tấm.
  const [presetId, setPresetId] = useState('warm');

  return (
    <View style={s.root}>
      <StatusBar barStyle="light-content" />
      {route.name === 'signin' && <SignInScreen onSignedIn={() => setRoute({ name: 'sessions' })} />}

      {route.name === 'sessions' && (
        <SessionsScreen
          onOpenSession={() => setRoute({ name: 'tether' })}
          onOpenSettings={() => setRoute({ name: 'storage' })}
        />
      )}

      {route.name === 'tether' && (
        <TetherScreen
          presetId={presetId}
          onOpenShot={shot => setRoute({ name: 'photo', shot })}
          onBack={() => setRoute({ name: 'sessions' })}
        />
      )}

      {route.name === 'photo' && (
        <PhotoScreen
          shot={route.shot}
          presetId={presetId}
          onPresetChange={setPresetId}
          onBack={() => setRoute({ name: 'tether' })}
          onClientMode={() => setRoute({ name: 'client', shot: route.shot })}
        />
      )}

      {route.name === 'client' && (
        <ClientReviewScreen
          shot={route.shot}
          presetId={presetId}
          onExit={() => setRoute({ name: 'photo', shot: route.shot })}
        />
      )}

      {route.name === 'storage' && <StorageScreen onBack={() => setRoute({ name: 'sessions' })} />}
    </View>
  );
}

const s = StyleSheet.create({
  root: { flex: 1, backgroundColor: colors.background },
});
