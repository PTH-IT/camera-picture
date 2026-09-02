import { useState } from 'react';
import { View, StyleSheet } from 'react-native';
import { App } from '@app/App';
import { SignInScreen } from '@app/screens/SignInScreen';
import { SessionsScreen } from '@app/screens/SessionsScreen';
import { TetherScreen } from '@app/screens/TetherScreen';
import { PhotoScreen } from '@app/screens/PhotoScreen';
import { StorageScreen } from '@app/screens/StorageScreen';
import { ClientReviewScreen } from '@app/screens/ClientReviewScreen';
import { ColorScreen } from '@app/screens/ColorScreen';
import { NEUTRAL_ADJUSTMENTS } from '@app/color/adjustments';
import {
  demoCamera,
  demoPresets,
  demoSessions,
  demoShots,
  demoStorageOptions,
  demoUsage,
} from '@app/demo/fixtures';
import type { StorageProvider } from '@app/account/types';

/**
 * Vỏ cho bản xem trước.
 *
 * Chọn màn hình bằng hash của URL (#photo, #tether...) để chụp màn hình từng cái
 * mà không phải bấm qua từng bước. Không có hash thì chạy ứng dụng thật với điều
 * hướng đầy đủ và gọi backend thật.
 *
 * Điểm quan trọng: các màn hình ở đây là ĐÚNG những màn hình chạy trên máy thật,
 * chỉ khác nguồn dữ liệu. Chúng thuần trình bày và nhận dữ liệu qua props, nên
 * bản xem trước không thể trôi lệch khỏi sản phẩm.
 *
 * Các màn hình dùng useWindowDimensions như mọi màn hình React Native đúng chuẩn,
 * nên cửa sổ trình duyệt PHẢI được đặt về kích thước điện thoại. Không có khung
 * điện thoại giả — nhét khung giả vào sẽ khiến useWindowDimensions trả sai và bố
 * cục ở đây khác bố cục trên máy thật, tức là bản xem trước nói dối.
 */
export function Harness() {
  const shots = demoShots(12);
  const shot = shots[0]!;
  const [presetId, setPresetId] = useState('warm');
  const [storage, setStorage] = useState<StorageProvider>('managed');
  const [adjustments, setAdjustments] = useState(NEUTRAL_ADJUSTMENTS);
  const [amount, setAmount] = useState(0.85);
  const [colorShotId, setColorShotId] = useState<string | null>(null);
  const [savedId, setSavedId] = useState<string | null>(null);
  const hash = typeof window !== 'undefined' ? window.location.hash.replace('#', '') : '';

  const screen = (() => {
    switch (hash) {
      case 'signin':
        return <SignInScreen onSubmit={() => {}} onProvider={() => {}} />;

      case 'sessions':
        return (
          <SessionsScreen
            sessions={demoSessions}
            onOpenSession={() => {}}
            onOpenSettings={() => {}}
          />
        );

      case 'tether':
        return (
          <TetherScreen
            title="Minh & Lan — Tiệc cưới"
            shots={shots}
            camera={demoCamera}
            presets={demoPresets}
            presetId={presetId}
            onOpenShot={() => {}}
            onBack={() => {}}
          />
        );

      case 'tether-empty':
        // Trạng thái rỗng cũng phải xem được: đây là màn hình đầu tiên người
        // dùng thấy khi mở buổi chụp mới, và nó dễ bị bỏ quên khi thiết kế.
        return (
          <TetherScreen
            title="Buổi chụp mới"
            shots={[]}
            camera={null}
            presets={demoPresets}
            presetId={presetId}
            onOpenShot={() => {}}
            onBack={() => {}}
          />
        );

      case 'tether-fulldownload':
        // Máy ảnh KHÔNG lấy được preview nếu chưa tải cả file RAW, và không đọc
        // được mức pin. Hai trạng thái này chỉ xuất hiện với một số đường capture
        // nhất định, nên nếu không xem trước được thì sẽ không ai nhìn thấy chúng
        // cho tới khi cắm đúng cái máy ảnh đó vào — lúc đó thì đã ở buổi chụp thật.
        return (
          <TetherScreen
            title="Minh & Lan — Tiệc cưới"
            shots={shots}
            camera={{ model: 'Nikon Z8', transport: 'usb' }}
            previewNeedsFullDownload
            presets={demoPresets}
            presetId={presetId}
            onOpenShot={() => {}}
            onBack={() => {}}
          />
        );

      case 'color':
        return (
          <ColorScreen
            shots={shots}
            selectedId={colorShotId}
            onSelect={setColorShotId}
            presets={demoPresets}
            presetId={presetId}
            onPresetChange={setPresetId}
            amount={amount}
            onAmountChange={setAmount}
            adjustments={adjustments}
            onAdjust={setAdjustments}
            onCommit={() => {}}
            onReset={() => setAdjustments(NEUTRAL_ADJUSTMENTS)}
            saved={[
              { id: 'p1', name: 'Cưới ấm' },
              { id: 'p2', name: 'Trong nhà 3200K' },
            ]}
            savedId={savedId}
            onApplySaved={setSavedId}
            onSavePreset={() => {}}
            onDeletePreset={() => setSavedId(null)}
            onExportDevice={() => {}}
            onExportStorage={() => {}}
            // Bản xem trước không xuất được: màu ở đây là xấp xỉ bằng CSS filter
            // nên file xuất ra sẽ khác hẳn thứ máy thật tạo.
            exportDisabledReason="Xuất ảnh chỉ chạy trên máy thật."
          />
        );

      case 'color-empty':
        // Trạng thái chưa có ảnh nào để chỉnh — màn hình đầu tiên người dùng
        // thấy nếu mở tab này trước khi mở buổi chụp.
        return (
          <ColorScreen
            shots={[]}
            selectedId={null}
            onSelect={() => {}}
            presets={demoPresets}
            presetId={presetId}
            onPresetChange={setPresetId}
            amount={amount}
            onAmountChange={setAmount}
            adjustments={adjustments}
            onAdjust={setAdjustments}
            onCommit={() => {}}
            onReset={() => {}}
            saved={[]}
            savedId={null}
            onApplySaved={() => {}}
            onSavePreset={() => {}}
            onDeletePreset={() => {}}
            onExportDevice={() => {}}
            onExportStorage={() => {}}
          />
        );

      case 'photo':
        return (
          <PhotoScreen
            shot={shot}
            presets={demoPresets}
            presetId={presetId}
            onPresetChange={setPresetId}
            onBack={() => {}}
            onClientMode={() => {}}
          />
        );

      case 'client':
        return (
          <ClientReviewScreen
            shot={shot}
            presets={demoPresets}
            presetId={presetId}
            onExit={() => {}}
          />
        );

      case 'storage':
        return (
          <StorageScreen
            options={demoStorageOptions}
            selected={storage}
            usage={demoUsage}
            onSelect={setStorage}
            onBack={() => {}}
          />
        );

      default:
        return <App />;
    }
  })();

  return <View style={s.fill}>{screen}</View>;
}

const s = StyleSheet.create({ fill: { flex: 1 } });
