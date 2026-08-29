import { useCallback, useMemo, useState } from 'react';
import { StatusBar, StyleSheet } from 'react-native';
import { SafeAreaProvider, SafeAreaView } from 'react-native-safe-area-context';
import { ApiClient, ApiError, memoryTokenStore } from './api/client';
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
  const [auth, setAuth] = useState<{ busy: boolean; error: string | null }>({
    busy: false,
    error: null,
  });

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

  /**
   * Đăng nhập bằng email.
   *
   * Chỗ này gọi máy chủ THẬT và chỉ chuyển màn hình khi đã có token. Trước đây
   * nút đăng nhập chỉ đổi state cục bộ, nên màn hình tiếp theo gọi API mà không
   * có token, nhận 401, và bị đá ngược về đây — trông như bấm nút không ăn.
   */
  const submitAuth = useCallback(
    async (email: string, password: string, mode: 'signIn' | 'signUp') => {
      setAuth({ busy: true, error: null });
      try {
        if (mode === 'signUp') await client.signUp(email, password);
        else await client.signIn(email, password);
        setAuth({ busy: false, error: null });
        setSignedIn(true);
        setRoute({ name: 'sessions' });
      } catch (e) {
        setAuth({ busy: false, error: authMessage(e) });
      }
    },
    [client],
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
    /*
     * Vùng an toàn xử lý ở ĐÚNG MỘT CHỖ này, không rải vào từng màn hình.
     *
     * Không có nó, tiêu đề của mọi màn hình nằm đè lên đồng hồ và tai thỏ. Đây
     * là loại lỗi mà bản xem trước trong trình duyệt KHÔNG BAO GIỜ chỉ ra được
     * — cửa sổ trình duyệt không có tai thỏ — nên nó chỉ lộ ra ở lần chạy đầu
     * tiên trên simulator.
     *
     * Dùng react-native-safe-area-context chứ không dùng `SafeAreaView` có sẵn
     * của React Native: bản có sẵn đã bị đánh dấu deprecated và sẽ bị gỡ, đồng
     * thời in cảnh báo vàng che mất màn hình trong mọi bản dev. Thư viện này
     * nằm sẵn trong template React Native 0.81 nên không phải lựa chọn lạ.
     */
    <SafeAreaProvider>
      <SafeAreaView style={s.root} edges={['top', 'bottom']}>
        <StatusBar barStyle="light-content" />

        {route.name === 'signin' && (
          <SignInScreen
            onSubmit={(email, password, mode) => void submitAuth(email, password, mode)}
            busy={auth.busy}
            error={auth.error}
            // Apple/Google cần SDK native để lấy ID token, bản build này chưa có.
            // Nói thẳng ra thay vì để nút bấm không phản ứng gì.
            onProvider={provider =>
              setAuth({
                busy: false,
                error: `Đăng nhập ${provider === 'apple' ? 'Apple' : 'Google'} chưa bật trong bản build này. Dùng email để tiếp tục.`,
              })
            }
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
      </SafeAreaView>
    </SafeAreaProvider>
  );
}

/**
 * Thông báo lỗi đăng nhập cho người đọc.
 *
 * Dịch theo MÃ lỗi chứ không hiện thẳng thông báo của máy chủ: mã là hợp đồng
 * ổn định, còn thông báo dành cho người viết code và có thể đổi bất cứ lúc nào.
 * Riêng `invalid_input` thì giữ nguyên lời máy chủ — chính nó nói rõ dữ liệu
 * nào sai, ví dụ mật khẩu chưa đủ dài.
 */
function authMessage(e: unknown): string {
  if (!(e instanceof ApiError)) return 'Không đăng nhập được. Thử lại.';

  switch (e.code) {
    case 'unauthorized':
      // Cố ý KHÔNG phân biệt sai email với sai mật khẩu: phân biệt là nói cho
      // người lạ biết email nào đã có tài khoản.
      return 'Email hoặc mật khẩu không đúng.';
    case 'conflict':
      return 'Email này đã có tài khoản. Hãy đăng nhập.';
    case 'network':
      return 'Không kết nối được máy chủ. Kiểm tra mạng rồi thử lại.';
    default:
      return e.message;
  }
}

const s = StyleSheet.create({
  root: { flex: 1, backgroundColor: colors.background },
});
