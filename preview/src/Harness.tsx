import { useState } from 'react';
import { View, StyleSheet } from 'react-native';
import { App } from '@app/App';
import { SignInScreen } from '@app/screens/SignInScreen';
import { SessionsScreen } from '@app/screens/SessionsScreen';
import { TetherScreen } from '@app/screens/TetherScreen';
import { PhotoScreen } from '@app/screens/PhotoScreen';
import { StorageScreen } from '@app/screens/StorageScreen';
import { ClientReviewScreen } from '@app/screens/ClientReviewScreen';
import { demoShots } from '@app/demo/fixtures';

/**
 * Vỏ cho bản xem trước.
 *
 * Chọn màn hình bằng hash của URL (#photo, #tether...) để chụp màn hình từng cái
 * mà không phải bấm qua từng bước. Không có hash thì chạy ứng dụng thật với điều
 * hướng đầy đủ.
 *
 * Các màn hình dùng useWindowDimensions như mọi màn hình React Native đúng chuẩn,
 * nên cửa sổ trình duyệt PHẢI được đặt về kích thước điện thoại. Không có khung
 * điện thoại giả — nhét khung giả vào sẽ khiến useWindowDimensions trả sai và bố
 * cục ở đây khác bố cục trên máy thật, tức là bản xem trước nói dối.
 */
export function Harness() {
  const shot = demoShots(12)[0]!;
  const [presetId, setPresetId] = useState('warm');
  const hash = typeof window !== 'undefined' ? window.location.hash.replace('#', '') : '';

  const screen = (() => {
    switch (hash) {
      case 'signin':
        return <SignInScreen onSignedIn={() => {}} />;
      case 'sessions':
        return <SessionsScreen onOpenSession={() => {}} onOpenSettings={() => {}} />;
      case 'tether':
        return <TetherScreen presetId={presetId} onOpenShot={() => {}} onBack={() => {}} />;
      case 'photo':
        return (
          <PhotoScreen
            shot={shot}
            presetId={presetId}
            onPresetChange={setPresetId}
            onBack={() => {}}
            onClientMode={() => {}}
          />
        );
      case 'client':
        return <ClientReviewScreen shot={shot} presetId={presetId} onExit={() => {}} />;
      case 'storage':
        return <StorageScreen onBack={() => {}} />;
      default:
        return <App />;
    }
  })();

  return <View style={s.fill}>{screen}</View>;
}

const s = StyleSheet.create({ fill: { flex: 1 } });
