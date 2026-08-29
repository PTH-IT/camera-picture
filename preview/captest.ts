/**
 * Kiểm thử adapter tầng capture, chạy bằng Node.
 *
 * Vì sao kiểm được ở đây mà không cần thiết bị: `mobile/src/capture/adapter.ts`
 * cố ý KHÔNG import `react-native`. Nó nhận vào một `Spec` (lớp vận chuyển) và
 * một hàm mở kênh sự kiện, nên chỗ này thay cả hai bằng bản giả và ép được đúng
 * những tình huống khó dựng bằng máy ảnh thật: payload hỏng, sự kiện lạ, hai bên
 * cùng tìm máy ảnh, hai tấm tải song song.
 *
 * Chạy:
 *   cd preview && npx tsx captest.ts
 *
 * Bản đối ứng chạy với backend thật là `itest.ts`. Hai bộ này không giao nhau:
 * một bộ giữ hợp đồng với máy chủ, bộ kia giữ hợp đồng với native.
 */

import { createCaptureSourceFrom, type NativeEventTap } from '@app/capture/adapter';
import { CaptureError, type CaptureEvent } from '@app/capture/types';

let passed = 0;
let failed = 0;

function check(name: string, ok: boolean, detail = '') {
  if (ok) {
    passed++;
    console.log(`  PASS  ${name}`);
  } else {
    failed++;
    console.log(`  FAIL  ${name}${detail ? ' — ' + detail : ''}`);
  }
}

async function expectError(name: string, fn: () => Promise<unknown>, code: string, inMessage = '') {
  try {
    await fn();
    check(name, false, `không ném lỗi, mong đợi ${code}`);
  } catch (e) {
    const got = e instanceof CaptureError ? e.code : `không phải CaptureError: ${String(e)}`;
    const msg = e instanceof Error ? e.message : '';
    check(name, got === code && msg.includes(inMessage), `nhận ${got} / "${msg}"`);
  }
}

// ---------------------------------------------------------------------------
// Bản giả của lớp vận chuyển
// ---------------------------------------------------------------------------

const cameraPayload = {
  id: 'cam-1',
  manufacturer: 'Nikon',
  model: 'Z 8',
  firmwareVersion: '2.10',
  transport: 'wifi',
  // `chưaBiếtTênGì` mô phỏng bản native mới hơn bản JS.
  capabilities: ['remoteShutter', 'previewWithoutFullDownload', 'chuaBietTenGi'],
};

function itemPayload(i: number, format = 'NEF') {
  return {
    id: `item-${i}`,
    filename: `DSC_${4000 + i}.NEF`,
    format,
    byteSize: 55 * 1024 * 1024,
    capturedAt: `2026-08-29T09:0${i}:00Z`,
    isRaw: true,
    hasEmbeddedPreview: true,
  };
}

const handlePayload = { uri: 'file:///tmp/p.jpg', width: 1620, height: 1080, byteSize: 400_000 };

interface Harness {
  calls: string[];
  emit: (payload: unknown) => void;
  taps: number;
}

function makeSource(overrides: Record<string, unknown> = {}) {
  const h: Harness = { calls: [], emit: () => {}, taps: 0 };

  const spec = {
    startDiscovery: () => void h.calls.push('startDiscovery'),
    stopDiscovery: () => void h.calls.push('stopDiscovery'),
    connect: async () => cameraPayload,
    disconnect: async () => {},
    listItems: async (_cam: string, after: string, limit: number) => {
      h.calls.push(`listItems(${after || '∅'},${limit})`);
      return [itemPayload(0)];
    },
    fetchPreview: async () => handlePayload,
    fetchOriginal: async () => handlePayload,
    triggerShutter: async () => itemPayload(9),
    startLiveView: async () => {},
    stopLiveView: async () => {},
    readSettings: async () => ({ iso: 400 }),
    writeSetting: async (_cam: string, _key: string, valueJson: string) => {
      h.calls.push(`writeSetting(${valueJson})`);
    },
    addListener: () => {},
    removeListeners: () => {},
    ...overrides,
  };

  const tap: NativeEventTap = onEvent => {
    h.taps++;
    h.emit = onEvent;
    return () => {};
  };

  // Bản giả cố ý không mang kiểu `Spec`: nó chỉ cần đủ hình dạng để chạy, và
  // buộc nó thoả cả interface TurboModule sẽ kéo react-native vào bộ kiểm thử.
  return { source: createCaptureSourceFrom(spec as never, tap), h };
}

const tick = () => new Promise(r => setTimeout(r, 0));

// ---------------------------------------------------------------------------

async function main() {
  console.log('Kiểm thử adapter tầng capture\n');

  // --- giải mã ---
  console.log('Giải mã');
  {
    const { source } = makeSource();
    const cam = await source.connect('cam-1');
    check('connect trả CameraInfo đã giải mã', cam.id === 'cam-1' && cam.model === 'Z 8');
    check('firmwareVersion đi qua nguyên vẹn', cam.firmwareVersion === '2.10');
    check(
      'khả năng lạ bị bỏ qua, không gây lỗi',
      cam.capabilities.length === 2 && !cam.capabilities.includes('chuaBietTenGi' as never),
      cam.capabilities.join(','),
    );
  }

  await expectError(
    'thiếu trường bắt buộc thì báo đúng tên trường',
    async () => {
      const { source } = makeSource({ connect: async () => ({ ...cameraPayload, id: 42 }) });
      return source.connect('x');
    },
    'unknown',
    'connect.id',
  );

  await expectError(
    'transport lạ bị từ chối',
    async () => {
      const { source } = makeSource({
        connect: async () => ({ ...cameraPayload, transport: 'bluetooth' }),
      });
      return source.connect('x');
    },
    'unknown',
    'connect.transport',
  );

  {
    const { source } = makeSource({ triggerShutter: async () => itemPayload(1, 'XYZ') });
    const item = await source.triggerShutter('cam-1');
    // Đuôi file lạ không được làm sập app: máy ảnh mới ra liên tục.
    check('định dạng lạ hạ về unknown', item.format === 'unknown', item.format);
  }

  // --- ánh xạ lỗi ---
  console.log('\nÁnh xạ lỗi');
  await expectError(
    'mã lỗi của native đi qua nguyên vẹn',
    async () => {
      const { source } = makeSource({
        connect: async () => {
          throw Object.assign(new Error('Máy ảnh đang bận'), { code: 'cameraBusy' });
        },
      });
      return source.connect('x');
    },
    'cameraBusy',
    'đang bận',
  );

  await expectError(
    'mã lỗi lạ hạ về unknown thay vì tin',
    async () => {
      const { source } = makeSource({
        connect: async () => {
          throw Object.assign(new Error('?'), { code: 'somethingNew' });
        },
      });
      return source.connect('x');
    },
    'unknown',
  );

  // --- tìm máy ảnh ---
  console.log('\nTìm máy ảnh');
  {
    const { source, h } = makeSource();
    const found: string[] = [];
    const seen: CaptureEvent[] = [];
    source.subscribe(e => void seen.push(e));

    const stopA = source.startDiscovery(c => void found.push('A:' + c.id));
    const stopB = source.startDiscovery(c => void found.push('B:' + c.id));
    check(
      'hai bên cùng tìm chỉ gọi native một lần',
      h.calls.filter(c => c === 'startDiscovery').length === 1,
      h.calls.join(','),
    );

    h.emit({ type: 'cameraDiscovered', camera: cameraPayload });
    check('cả hai bên đều nhận được máy ảnh', found.join(' ') === 'A:cam-1 B:cam-1', found.join(' '));
    check('sự kiện discovery KHÔNG lẫn vào luồng sự kiện chung', seen.length === 0);

    stopA();
    check('bên còn lại vẫn đang tìm', !h.calls.includes('stopDiscovery'));
    stopA();
    stopB();
    check(
      'huỷ hết mới dừng, và huỷ hai lần không đếm nhầm',
      h.calls.filter(c => c === 'stopDiscovery').length === 1,
      h.calls.join(','),
    );
  }

  // --- luồng sự kiện ---
  console.log('\nLuồng sự kiện');
  {
    const { source, h } = makeSource();
    const seen: CaptureEvent[] = [];
    const off = source.subscribe(e => void seen.push(e));
    source.subscribe(() => {});
    check('kênh native chỉ mở một lần', h.taps === 1, `${h.taps} lần`);

    h.emit({ type: 'itemCaptured', item: itemPayload(0) });
    h.emit({ type: 'itemCaptured', item: itemPayload(0), preview: handlePayload });
    const first = seen[0];
    const second = seen[1];
    check(
      'ảnh mới tới trước, chưa kèm preview',
      first?.type === 'itemCaptured' && first.preview === undefined,
    );
    check(
      'preview tới sau cùng một item',
      second?.type === 'itemCaptured' && second.preview?.uri === handlePayload.uri,
    );

    h.emit({ type: 'somethingFromTheFuture', payload: 1 });
    check('sự kiện chưa biết bị bỏ qua, không nổ', seen.length === 2, `${seen.length} sự kiện`);

    h.emit({ type: 'itemCaptured', item: { id: 'x' } });
    const third = seen[2];
    check(
      'payload hỏng thành sự kiện lỗi chứ không giết cầu sự kiện',
      third?.type === 'error' && third.message.includes('itemCaptured.item.filename'),
      third?.type === 'error' ? third.message : third?.type,
    );

    off();
    h.emit({ type: 'liveViewFrame', handle: handlePayload });
    check('huỷ đăng ký thì ngừng nhận', seen.length === 3, `${seen.length} sự kiện`);
  }

  {
    // Listener tự huỷ ngay trong lúc đang phát sự kiện: sửa tập hợp đang lặp là
    // lỗi kinh điển, và nó chỉ lộ ra đúng vào tình huống này.
    const { source, h } = makeSource();
    let count = 0;
    const off = source.subscribe(() => {
      count++;
      off();
    });
    source.subscribe(() => void count++);
    h.emit({ type: 'cameraDisconnected', cameraId: 'cam-1', reason: 'rớt Wi-Fi' });
    check('huỷ đăng ký giữa lúc phát sự kiện vẫn an toàn', count === 2, `${count}`);
  }

  // --- tiến độ tải ---
  console.log('\nTiến độ tải');
  {
    // Mỗi lần tải giữ một hàm giải phóng RIÊNG, theo itemId: dùng chung một biến
    // sẽ khiến lần gọi thứ hai đè lên lần thứ nhất và bài kiểm thử treo.
    const releases = new Map<string, () => void>();
    const { source, h } = makeSource({
      fetchOriginal: async (_cam: string, itemId: string) =>
        new Promise(resolve => {
          releases.set(itemId, () => resolve(handlePayload));
        }),
    });

    const a: number[] = [];
    const b: number[] = [];
    const pending = source.fetchOriginal('cam-1', 'item-0', { onProgress: d => void a.push(d) });
    source.fetchOriginal('cam-1', 'item-1', { onProgress: d => void b.push(d) }).catch(() => {});
    await tick();

    h.emit({ type: 'transferProgress', itemId: 'item-0', bytesTransferred: 10, bytesTotal: 100 });
    check('tiến độ về đúng tấm đang tải', a.join(',') === '10', a.join(','));
    check('tấm khác không nhận nhầm tiến độ', b.length === 0);

    releases.get('item-0')?.();
    releases.get('item-1')?.();
    await pending;
    h.emit({ type: 'transferProgress', itemId: 'item-0', bytesTransferred: 20, bytesTotal: 100 });
    check('tải xong thì gỡ đăng ký tiến độ', a.join(',') === '10', a.join(','));
  }

  // --- lớp vận chuyển ---
  console.log('\nLớp vận chuyển');
  {
    const { source, h } = makeSource();
    await source.listItems('cam-1');
    check('không có con trỏ thì gửi chuỗi rỗng', h.calls.includes('listItems(∅,100)'), h.calls.join(','));

    await source.listItems('cam-1', { after: 'item-3', limit: 20 });
    check('con trỏ và giới hạn đi qua đúng', h.calls.includes('listItems(item-3,20)'));

    await source.writeSetting('cam-1', 'iso', 800);
    await source.writeSetting('cam-1', 'iso', undefined);
    check('giá trị được đóng gói JSON', h.calls.includes('writeSetting(800)'), h.calls.join(','));
    check('undefined hạ về null thay vì chuỗi "undefined"', h.calls.includes('writeSetting(null)'));
  }

  await expectError(
    'phần tử hỏng trong danh sách chỉ ra đúng vị trí',
    async () => {
      const { source } = makeSource({ listItems: async () => [itemPayload(0), { id: 'x' }] });
      return source.listItems('cam-1');
    },
    'unknown',
    'listItems[1].filename',
  );

  console.log(`\n${passed} đạt, ${failed} hỏng`);
  process.exit(failed === 0 ? 0 : 1);
}

main().catch(e => {
  console.error('\nLỗi không mong đợi:', e);
  process.exit(1);
});
