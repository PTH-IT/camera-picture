import {
  CaptureError,
  type CameraCapability,
  type CameraInfo,
  type CaptureErrorCode,
  type CaptureEvent,
  type CaptureItemRef,
  type CaptureSource,
  type ImageFormat,
  type ImageHandle,
  type Transport,
} from './types';
import type { Spec } from './NativeCaptureSource';

/**
 * Adapter giữa lớp vận chuyển native (`Spec`, kiểu nghèo do codegen ép) và hợp
 * đồng mà app dùng (`CaptureSource`, kiểu giàu).
 *
 * Vì sao phải có hẳn một tầng thay vì ép kiểu cho nhanh:
 *
 *  - Codegen của React Native chỉ cho phép `Object`/`Object[]` ở biên giới. Ép
 *    kiểu là NÓI DỐI trình biên dịch: dữ liệu vẫn là bất kỳ thứ gì native gửi
 *    sang, và sai sót sẽ nổ ở một màn hình cách đó ba tầng với thông báo vô
 *    nghĩa. Ở đây nó nổ ngay tại biên, kèm tên trường.
 *  - Một native module chỉ có MỘT kênh sự kiện. Discovery, ảnh mới, tiến độ
 *    truyền và lỗi đều chảy chung qua đó; tách chúng về đúng nơi nhận là việc
 *    của tầng này, không phải của từng màn hình.
 *
 * File này KHÔNG import `react-native`. Đó là chủ ý: nhờ vậy nó chạy được dưới
 * Node và bộ kiểm thử (`preview/captest.ts`) kiểm được toàn bộ logic giải mã,
 * đếm tham chiếu và định tuyến sự kiện mà không cần thiết bị hay simulator.
 */

/** Mở kênh sự kiện native. Trả hàm đóng kênh. */
export type NativeEventTap = (onEvent: (payload: unknown) => void) => () => void;

type Dict = Record<string, unknown>;

const CAPABILITIES: readonly string[] = [
  'remoteShutter',
  'liveView',
  'settingsRead',
  'settingsWrite',
  'tetheredEvents',
  'storageBrowse',
  'previewWithoutFullDownload',
  'videoRecord',
];

const FORMATS: readonly string[] = ['NEF', 'NRW', 'JPEG', 'HEIF', 'TIFF', 'MOV', 'MP4'];

const ERROR_CODES: readonly string[] = [
  'permissionDenied',
  'cameraBusy',
  'connectionLost',
  'unsupportedOperation',
  'storageReadFailed',
  'transferFailed',
  'licenseInvalid',
  'unknown',
];

// ---------------------------------------------------------------------------
// Giải mã
//
// Mọi hàm dưới đây ném `CaptureError` kèm ĐƯỜNG DẪN tới trường sai. Một payload
// hỏng từ native là lỗi lập trình, và thông báo "itemCaptured.item.byteSize:
// mong đợi number" chỉ ra chỗ sửa trong vài giây, còn "undefined is not an
// object" thì không.
// ---------------------------------------------------------------------------

function fail(where: string, detail: string): never {
  throw new CaptureError('unknown', `${where}: ${detail}`);
}

function dict(v: unknown, where: string): Dict {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) {
    fail(where, `mong đợi object, nhận ${v === null ? 'null' : typeof v}`);
  }
  return v as Dict;
}

function str(d: Dict, key: string, where: string): string {
  const v = d[key];
  if (typeof v !== 'string') fail(`${where}.${key}`, `mong đợi string, nhận ${typeof v}`);
  return v;
}

function num(d: Dict, key: string, where: string): number {
  const v = d[key];
  if (typeof v !== 'number' || !Number.isFinite(v)) {
    fail(`${where}.${key}`, `mong đợi number, nhận ${typeof v}`);
  }
  return v;
}

function bool(d: Dict, key: string, where: string): boolean {
  const v = d[key];
  if (typeof v !== 'boolean') fail(`${where}.${key}`, `mong đợi boolean, nhận ${typeof v}`);
  return v;
}

function optStr(d: Dict, key: string): string | undefined {
  const v = d[key];
  return typeof v === 'string' ? v : undefined;
}

export function decodeCamera(v: unknown, where = 'camera'): CameraInfo {
  const d = dict(v, where);
  const transport = str(d, 'transport', where);
  if (transport !== 'usb' && transport !== 'wifi') {
    // Transport lạ là lỗi phía native, không phải dữ liệu người dùng. Ném thay
    // vì đoán: UI rẽ nhánh theo transport (Wi-Fi chậm hơn USB nhiều lần), và
    // đoán sai ở đây sẽ hiện cảnh báo sai cho người đang chụp.
    fail(`${where}.transport`, `giá trị lạ "${transport}"`);
  }

  const rawCaps = d.capabilities;
  if (!Array.isArray(rawCaps)) fail(`${where}.capabilities`, 'mong đợi mảng');

  // Khả năng lạ bị BỎ QUA chứ không gây lỗi: bản native mới hơn bản JS là
  // chuyện bình thường khi người dùng chưa cập nhật app. UI chỉ hỏi "có khả
  // năng X không", nên một khả năng chưa biết tên đơn giản là chưa dùng tới.
  const capabilities = rawCaps.filter(
    (c): c is CameraCapability => typeof c === 'string' && CAPABILITIES.includes(c),
  );

  return {
    id: str(d, 'id', where),
    manufacturer: str(d, 'manufacturer', where),
    model: str(d, 'model', where),
    firmwareVersion: optStr(d, 'firmwareVersion'),
    transport: transport as Transport,
    capabilities,
  };
}

export function decodeItem(v: unknown, where = 'item'): CaptureItemRef {
  const d = dict(v, where);
  // Đọc theo đúng thứ tự khai báo để trường thiếu ĐẦU TIÊN là trường được báo:
  // "item.filename" chỉ thẳng vào chỗ sai, còn một trường ngẫu nhiên ở giữa thì không.
  const id = str(d, 'id', where);
  const filename = str(d, 'filename', where);
  const format = str(d, 'format', where);
  return {
    id,
    filename,
    // Đuôi file lạ KHÔNG được làm sập app: máy ảnh mới ra đời liên tục, và một
    // tấm không đọc được định dạng vẫn phải hiện trong lưới với tên của nó.
    format: (FORMATS.includes(format) ? format : 'unknown') as ImageFormat,
    byteSize: num(d, 'byteSize', where),
    capturedAt: str(d, 'capturedAt', where),
    isRaw: bool(d, 'isRaw', where),
    hasEmbeddedPreview: bool(d, 'hasEmbeddedPreview', where),
  };
}

export function decodeHandle(v: unknown, where = 'handle'): ImageHandle {
  const d = dict(v, where);
  return {
    uri: str(d, 'uri', where),
    width: num(d, 'width', where),
    height: num(d, 'height', where),
    byteSize: num(d, 'byteSize', where),
  };
}

/**
 * Giải mã một sự kiện native.
 *
 * Trả `null` cho sự kiện mà tầng này KHÔNG chuyển tiếp: `cameraDiscovered` đi
 * riêng về callback của `startDiscovery`, và mọi `type` chưa biết bị bỏ qua để
 * bản JS cũ hơn native không bị vỡ.
 */
export function decodeEvent(payload: unknown): CaptureEvent | null {
  const d = dict(payload, 'event');
  const type = typeof d.type === 'string' ? d.type : '';

  switch (type) {
    case 'cameraConnected':
      return { type: 'cameraConnected', camera: decodeCamera(d.camera, 'cameraConnected.camera') };

    case 'cameraDisconnected':
      return {
        type: 'cameraDisconnected',
        cameraId: str(d, 'cameraId', type),
        reason: str(d, 'reason', type),
      };

    case 'itemCaptured': {
      const item = decodeItem(d.item, 'itemCaptured.item');
      // Preview vắng mặt là trạng thái BÌNH THƯỜNG, không phải lỗi: native bắn
      // sự kiện ngay khi bấm máy rồi bắn lại cùng item kèm ảnh khi có. Cả điểm
      // hấp dẫn của tether nằm ở chỗ lưới ảnh nhúc nhích trong vòng một giây.
      if (d.preview === undefined || d.preview === null) return { type: 'itemCaptured', item };
      return {
        type: 'itemCaptured',
        item,
        preview: decodeHandle(d.preview, 'itemCaptured.preview'),
      };
    }

    case 'transferProgress':
      return {
        type: 'transferProgress',
        itemId: str(d, 'itemId', type),
        bytesTransferred: num(d, 'bytesTransferred', type),
        bytesTotal: num(d, 'bytesTotal', type),
      };

    case 'liveViewFrame':
      return { type: 'liveViewFrame', handle: decodeHandle(d.handle, 'liveViewFrame.handle') };

    case 'settingsChanged':
      return { type: 'settingsChanged', changed: dict(d.changed, 'settingsChanged.changed') };

    case 'error': {
      const code = str(d, 'code', type);
      return {
        type: 'error',
        // Mã lạ hạ về `unknown`: một chuỗi ngoài union sẽ khiến `switch` ở tầng
        // UI rơi vào nhánh không tồn tại.
        code: (ERROR_CODES.includes(code) ? code : 'unknown') as CaptureErrorCode,
        message: str(d, 'message', type),
        itemId: optStr(d, 'itemId'),
      };
    }

    default:
      return null;
  }
}

/**
 * Chuyển lỗi bất kỳ thành `CaptureError`.
 *
 * Promise bị từ chối từ native mang `code` là một `CaptureErrorCode` — module
 * Swift đã ánh xạ sẵn (xem `settle` trong CaptureSourceModule.swift). Mã lạ bị
 * hạ về `unknown` thay vì tin: một chuỗi không nằm trong union sẽ khiến `switch`
 * ở tầng UI rơi vào nhánh không tồn tại.
 */
export function toCaptureError(e: unknown, itemId?: string): CaptureError {
  if (e instanceof CaptureError) return e;

  const raw = typeof e === 'object' && e !== null ? (e as Dict) : {};
  const code = typeof raw.code === 'string' && ERROR_CODES.includes(raw.code) ? raw.code : 'unknown';
  const message =
    typeof raw.message === 'string' && raw.message ? raw.message : `lỗi không rõ: ${String(e)}`;

  return new CaptureError(code as CaptureErrorCode, message, itemId);
}

// ---------------------------------------------------------------------------
// Adapter
// ---------------------------------------------------------------------------

/** Bọc handler trong object riêng để hai lần đăng ký cùng một hàm vẫn đếm là hai. */
interface Slot<T> {
  fn: T;
}

export function createCaptureSourceFrom(spec: Spec, tap: NativeEventTap): CaptureSource {
  const listeners = new Set<Slot<(event: CaptureEvent) => void>>();
  const finders = new Set<Slot<(camera: CameraInfo) => void>>();
  const progress = new Map<string, Set<(done: number, total: number) => void>>();

  let attached: (() => void) | null = null;

  /**
   * Mở kênh native ở lần dùng đầu tiên, và KHÔNG bao giờ đóng lại.
   *
   * Đóng kênh khi không còn ai nghe là tối ưu sai chỗ: máy ảnh vẫn bắn sự kiện
   * trong lúc kênh đóng, và một tấm ảnh chụp đúng khoảng đó biến mất không dấu
   * vết — không có API nào phát lại. Chi phí giữ kênh mở là một listener.
   */
  function attach(): void {
    if (attached) return;
    attached = tap(dispatch);
  }

  /**
   * Gọi một handler của tầng trên mà không để lỗi của nó kéo theo cả cầu sự kiện.
   *
   * Một màn hình ném lỗi trong lúc xử lý ảnh mới KHÔNG được làm ngừng luồng sự
   * kiện: hậu quả là tether chết im lặng giữa buổi chụp, ảnh vẫn chụp mà không
   * tấm nào về nữa. Lỗi được ném lại ở vòng lặp sự kiện kế tiếp, nên nó vẫn nổ
   * to như thường trong lúc phát triển thay vì bị nuốt mất.
   */
  function safely<T>(fn: (arg: T) => void, arg: T): void {
    try {
      fn(arg);
    } catch (e) {
      setTimeout(() => {
        throw e;
      }, 0);
    }
  }

  function dispatch(payload: unknown): void {
    let event: CaptureEvent | null;
    try {
      const d = dict(payload, 'event');
      if (d.type === 'cameraDiscovered') {
        const camera = decodeCamera(d.camera, 'cameraDiscovered.camera');
        // Sao chép trước khi lặp: một handler hoàn toàn có thể huỷ discovery
        // ngay khi tìm thấy máy đầu tiên, và sửa Set đang lặp là lỗi kinh điển.
        for (const slot of [...finders]) safely(slot.fn, camera);
        return;
      }
      event = decodeEvent(d);
    } catch (e) {
      // Payload hỏng không được làm chết cầu sự kiện. Ném ở đây sẽ trồi lên
      // NativeEventEmitter và giết luôn mọi sự kiện sau đó — mất tether im lặng
      // giữa buổi chụp. Biến nó thành sự kiện lỗi để UI thấy được.
      event = { type: 'error', code: 'unknown', message: toCaptureError(e).message };
    }

    if (!event) return;

    if (event.type === 'transferProgress') {
      const waiting = progress.get(event.itemId);
      if (waiting) {
        const done = event.bytesTransferred;
        const total = event.bytesTotal;
        for (const cb of [...waiting]) safely(() => cb(done, total), undefined);
      }
    }

    for (const slot of [...listeners]) safely(slot.fn, event);
  }

  async function guarded<T>(run: () => Promise<T>, itemId?: string): Promise<T> {
    try {
      return await run();
    } catch (e) {
      throw toCaptureError(e, itemId);
    }
  }

  return {
    startDiscovery(onFound) {
      attach();
      const slot: Slot<(camera: CameraInfo) => void> = { fn: onFound };
      finders.add(slot);

      // Đếm tham chiếu: hai màn hình cùng tìm máy ảnh là chuyện bình thường, và
      // màn hình đóng trước KHÔNG được tắt discovery của màn hình còn lại.
      if (finders.size === 1) spec.startDiscovery();

      let cancelled = false;
      return () => {
        if (cancelled) return;
        cancelled = true;
        finders.delete(slot);
        if (finders.size === 0) spec.stopDiscovery();
      };
    },

    subscribe(listener) {
      attach();
      const slot: Slot<(event: CaptureEvent) => void> = { fn: listener };
      listeners.add(slot);
      return () => {
        listeners.delete(slot);
      };
    },

    connect(cameraId) {
      return guarded(async () => decodeCamera(await spec.connect(cameraId), 'connect'));
    },

    disconnect(cameraId) {
      return guarded(async () => {
        await spec.disconnect(cameraId);
      });
    },

    listItems(cameraId, opts) {
      return guarded(async () => {
        // Chuỗi rỗng nghĩa là "từ đầu": codegen không nhận tham số string
        // nullable, nên `after` bên native là chuỗi chứ không phải null.
        const raw = await spec.listItems(cameraId, opts?.after ?? '', opts?.limit ?? 100);
        if (!Array.isArray(raw)) fail('listItems', 'mong đợi mảng');
        return raw.map((item, i) => decodeItem(item, `listItems[${i}]`));
      });
    },

    fetchPreview(cameraId, itemId) {
      return guarded(
        async () => decodeHandle(await spec.fetchPreview(cameraId, itemId), 'fetchPreview'),
        itemId,
      );
    },

    async fetchOriginal(cameraId, itemId, opts) {
      const onProgress = opts?.onProgress;

      // Tiến độ tới bằng SỰ KIỆN chứ không bằng promise (một NEF 55MB qua Wi-Fi
      // mất hàng chục giây). Đăng ký theo itemId để hai lần tải song song không
      // bắn tiến độ của nhau — thanh tiến độ nhảy lùi là lỗi rất khó truy.
      let waiting: Set<(done: number, total: number) => void> | undefined;
      if (onProgress) {
        attach();
        waiting = progress.get(itemId) ?? new Set();
        waiting.add(onProgress);
        progress.set(itemId, waiting);
      }

      try {
        return await guarded(
          async () => decodeHandle(await spec.fetchOriginal(cameraId, itemId), 'fetchOriginal'),
          itemId,
        );
      } finally {
        if (onProgress && waiting) {
          waiting.delete(onProgress);
          if (waiting.size === 0) progress.delete(itemId);
        }
      }
    },

    triggerShutter(cameraId) {
      return guarded(async () => decodeItem(await spec.triggerShutter(cameraId), 'triggerShutter'));
    },

    startLiveView(cameraId) {
      return guarded(async () => {
        await spec.startLiveView(cameraId);
      });
    },

    stopLiveView(cameraId) {
      return guarded(async () => {
        await spec.stopLiveView(cameraId);
      });
    },

    readSettings(cameraId) {
      return guarded(async () => dict(await spec.readSettings(cameraId), 'readSettings'));
    },

    writeSetting(cameraId, key, value) {
      return guarded(async () => {
        // `undefined` không có biểu diễn JSON; gửi nguyên sẽ thành chuỗi
        // "undefined" và native ném lỗi phân tích khó hiểu. Hạ về null tại đây.
        await spec.writeSetting(cameraId, key, JSON.stringify(value ?? null));
      });
    },
  };
}
