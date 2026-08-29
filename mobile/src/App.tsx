import { useCallback, useMemo, useState } from 'react';
import { StatusBar, StyleSheet, View } from 'react-native';
import { ApiClient, memoryTokenStore } from './api/client';
import { SignInScreen } from './screens/SignInScreen';
import { SessionsScreen } from './screens/SessionsScreen';
import { TetherScreen } from './screens/TetherScreen';
import { PhotoScreen } from './screens/PhotoScreen';
import { StorageScreen } from './screens/StorageScreen';
import { ClientReviewScreen } from './screens/ClientReviewScreen';
import { useSessions, useSessionSync, useStorage } from './state/store';
import { toTetherShotView, useTether } from './state/capture';
import { toShotView, type PresetView, type SessionView, type ShotView } from './screens/types';
import { demoPresets } from './demo/fixtures';
import { colors } from './ui/theme';

type Route =
  | { name: 'signin' }
  | { name: 'sessions' }
  | { name: 'tether'; sessionId: string; title: string }
  | { name: 'photo'; shot: ShotView }
  | { name: 'client'; shot: ShotView }
  | { name: 'storage' };

export interface AppProps {
  /** Địa chỉ backend. Đổi theo môi trường build. */
  baseUrl?: string;
  /**
   * Preset màu.
   *
   * Tạm lấy từ dữ liệu mẫu: backend chưa có endpoint phát hành preset, và
   * `.NP2/.NP3` của Nikon là định dạng nhị phân không có tài liệu nên không sinh
   * được bằng code. Xem references/nikon.md trong skill.
   */
  presets?: PresetView[];
}

/**
 * Vỏ ứng dụng và điều hướng.
 *
 * Cố ý dùng một máy trạng thái đơn giản thay vì react-navigation. Ứng dụng có
 * năm màn hình và luồng tuyến tính; thêm một thư viện điều hướng đầy đủ là mang
 * theo native module, cấu hình riêng cho từng nền tảng, và mất khả năng xem
 * trước trên trình duyệt — đổi lấy những tính năng chưa cần đến.
 *
 * Khi luồng phức tạp lên (deep link, tab, quay lại theo lịch sử), hãy thay bằng
 * react-navigation. Đừng cố kéo dài cái này.
 */
export function App({ baseUrl = 'http://127.0.0.1:8420', presets = demoPresets }: AppProps) {
  const [route, setRoute] = useState<Route>({ name: 'signin' });
  const [signedIn, setSignedIn] = useState(false);

  // Preset chọn ở màn hình ảnh phải giữ nguyên khi quay lại lưới: nhiếp ảnh gia
  // chọn một look cho cả buổi chụp, không phải cho từng tấm.
  const [presetId, setPresetId] = useState(presets[1]?.id ?? presets[0]?.id ?? 'none');

  const client = useMemo(
    () =>
      new ApiClient({
        baseUrl,
        tokens: memoryTokenStore(),
        // Phiên hết hạn giữa chừng phải đưa người dùng về đăng nhập ngay, không
        // để họ bấm tiếp và nhận lỗi ở từng thao tác.
        onUnauthorized: () => {
          setSignedIn(false);
          setRoute({ name: 'signin' });
        },
      }),
    [baseUrl],
  );

  const active = signedIn ? client : null;
  const sessions = useSessions(active);
  const storage = useStorage(active);

  const sessionId = route.name === 'tether' ? route.sessionId : null;
  const sync = useSessionSync(active, sessionId);

  const tether = useTether(active, sessionId);

  // Ghép ba nguồn thành một lưới ảnh: bản ghi đã đồng bộ, chỉnh sửa của chúng,
  // và những tấm vừa bấm mà máy chủ còn chưa biết tới.
  //
  // Preview cục bộ được ưu tiên hơn asset trên máy chủ. Đó không phải chuyện
  // tốc độ mà là chuyện đúng sai: phần lớn ảnh của một buổi chụp KHÔNG BAO GIỜ
  // lên máy chủ (docs/adr/0001-capture-strategy.md), nên nếu chỉ nhìn vào asset
  // thì lưới sẽ trống gần hết trong khi ảnh đang nằm sẵn trong máy.
  const shots: ShotView[] = useMemo(() => {
    const synced = sync.images.map(img =>
      toShotView(
        img,
        sync.edits.get(img.id),
        tether.previews.get(img.clientId) ?? img.assets?.preview?.storageKey ?? '',
      ),
    );

    // Tấm vừa bấm, máy chủ chưa biết: đặt lên đầu. Chúng mới nhất theo đúng
    // nghĩa đen — chưa kịp đi hết một vòng đẩy metadata.
    const known = new Set(sync.images.map(img => img.clientId));
    const fresh = tether.shots.filter(s => !known.has(s.clientId)).map(toTetherShotView);

    return [...fresh, ...synced];
  }, [sync.images, sync.edits, tether.shots, tether.previews]);

  const sessionViews: SessionView[] = useMemo(
    () =>
      (sessions.data ?? []).map(s => ({
        id: s.ID,
        name: s.Name,
        client: s.ClientName,
        date: new Date(s.StartedAt).toLocaleDateString('vi-VN'),
        shots: 0,
        live: false,
      })),
    [sessions.data],
  );

  const openSession = useCallback(
    (id: string) => {
      const found = sessionViews.find(s => s.id === id);
      setRoute({ name: 'tether', sessionId: id, title: found?.name ?? 'Buổi chụp' });
    },
    [sessionViews],
  );

  const newSession = useCallback(async () => {
    if (!active) return;
    const created = await active.createSession('Buổi chụp mới');
    sessions.reload();
    setRoute({ name: 'tether', sessionId: created.ID, title: created.Name });
  }, [active, sessions]);

  return (
    <View style={s.root}>
      <StatusBar barStyle="light-content" />

      {route.name === 'signin' && (
        <SignInScreen
          onSignedIn={() => {
            setSignedIn(true);
            setRoute({ name: 'sessions' });
          }}
        />
      )}

      {route.name === 'sessions' && (
        <SessionsScreen
          sessions={sessionViews}
          loading={sessions.loading}
          onOpenSession={openSession}
          onNewSession={() => void newSession()}
          onOpenSettings={() => setRoute({ name: 'storage' })}
        />
      )}

      {route.name === 'tether' && (
        <TetherScreen
          title={route.title}
          shots={shots}
          camera={tether.camera}
          previewNeedsFullDownload={tether.previewNeedsFullDownload}
          presets={presets}
          presetId={presetId}
          onOpenShot={shot => setRoute({ name: 'photo', shot })}
          onBack={() => setRoute({ name: 'sessions' })}
        />
      )}

      {route.name === 'photo' && (
        <PhotoScreen
          shot={route.shot}
          presets={presets}
          presetId={presetId}
          onPresetChange={setPresetId}
          onEdit={patch => void sync.putEdit(route.shot.id, patch)}
          onBack={() => setRoute({ name: 'sessions' })}
          onClientMode={() => setRoute({ name: 'client', shot: route.shot })}
        />
      )}

      {route.name === 'client' && (
        <ClientReviewScreen
          shot={route.shot}
          presets={presets}
          presetId={presetId}
          onExit={() => setRoute({ name: 'photo', shot: route.shot })}
        />
      )}

      {route.name === 'storage' && (
        <StorageScreen
          options={storage.options}
          selected={storage.selected}
          usage={storage.usage}
          onSelect={p => void storage.select(p)}
          onBack={() => setRoute({ name: 'sessions' })}
        />
      )}
    </View>
  );
}

const s = StyleSheet.create({
  root: { flex: 1, backgroundColor: colors.background },
});
