import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ApiClient, ImageInput } from '../api/client';
import {
  can,
  createCaptureSource,
  getTetherAvailability,
  type CameraInfo,
  type CaptureItemRef,
  type TetherAvailability,
} from '../capture';
import { formatTime, type CameraView, type ShotView } from '../screens/types';

/**
 * Tầng trạng thái nối tầng capture vào màn hình.
 *
 * Ranh giới ở đây giống hệt `store.ts`: màn hình vẫn THUẦN TRÌNH BÀY và không
 * biết gì về máy ảnh. Toàn bộ việc tìm máy, kết nối, nhận ảnh và đẩy metadata
 * lên máy chủ nằm trong hook này.
 *
 * Ba điều quyết định hình dạng của file:
 *
 *  1. **Ảnh phải hiện dưới một giây.** Sự kiện `itemCaptured` tới trước, preview
 *     tới sau — có khi là sự kiện thứ hai cùng item, có khi phải tự đi lấy. Lưới
 *     hiện ô trống ngay rồi điền ảnh vào, không chờ.
 *  2. **Metadata lên máy chủ, pixel thì không.** Mỗi tấm chỉ đẩy vài trăm byte
 *     mô tả; file RAW ở lại trên thẻ (docs/adr/0001-capture-strategy.md).
 *  3. **Buổi chụp thật hay rớt mạng.** Hàng đợi đẩy metadata gửi lại mù khi lỗi;
 *     `images/batch` idempotent theo `clientId` nên gửi trùng là vô hại, còn đoán
 *     xem lần trước đã tới nơi chưa thì không bao giờ đoán đúng.
 */

/** Gom ảnh trước khi đẩy: một buổi chụp liên thanh bắn ra vài tấm mỗi giây. */
const BATCH_DELAY_MS = 800;
/** Chờ lâu hơn khi lần đẩy trước hỏng — mạng ở nơi chụp thường chập chờn. */
const RETRY_DELAY_MS = 5_000;
/** Trần một lô. Trùng với giới hạn phía server. */
const MAX_BATCH = 200;
/**
 * Chờ preview do native tự bắn về trước khi tự đi lấy.
 *
 * Native gửi lại cùng một item kèm ảnh khi đã có, nên hỏi ngay lập tức là làm
 * hai lần cùng một việc. Chỉ khi im lặng quá lâu mới tự lấy.
 */
const PREVIEW_FALLBACK_MS = 2_000;

/** Một tấm vừa bấm, chưa chắc đã có trên máy chủ. */
export interface TetherShot {
  /** Id của item trên thẻ nhớ. Đây chính là `clientId` khi đẩy lên máy chủ. */
  readonly clientId: string;
  readonly filename: string;
  readonly capturedAt: string;
  /** null khi preview chưa về. Trạng thái bình thường trong khoảnh khắc đầu. */
  readonly previewUri: string | null;
}

export interface TetherState {
  readonly availability: TetherAvailability;
  readonly camera: CameraView | null;
  /** Mới nhất lên đầu. */
  readonly shots: TetherShot[];
  /** Tra preview theo `clientId`, để ghép với bản ghi đã đồng bộ. */
  readonly previews: ReadonlyMap<string, string>;
  /**
   * Máy ảnh này KHÔNG lấy được preview nếu chưa tải cả file RAW.
   *
   * Giao diện phải nói ra điều đó: 2000 tấm × 55MB sẽ không bao giờ xong, và
   * người dùng cần biết trước khi trách app chạy chậm.
   */
  readonly previewNeedsFullDownload: boolean;
  readonly error: string | null;
}

/** Chuyển tấm vừa bấm sang mô hình hiển thị chung của các màn hình. */
export function toTetherShotView(shot: TetherShot): ShotView {
  return {
    id: shot.clientId,
    filename: shot.filename,
    uri: shot.previewUri ?? '',
    time: formatTime(shot.capturedAt),
    rating: 0,
    flagged: false,
    rejected: false,
    // Vừa rời khỏi thẻ nhớ thì chắc chắn chưa có bản gốc trên máy chủ.
    originalUploaded: false,
  };
}

function toImageInput(item: CaptureItemRef, cameraId: string): ImageInput {
  return {
    clientId: item.id,
    filename: item.filename,
    format: item.format,
    byteSize: item.byteSize,
    capturedAt: item.capturedAt,
    isRaw: item.isRaw,
    cameraId,
  };
}

/** Đọc mức pin từ bảng thông số, chấp nhận vài cách đặt tên khác nhau. */
function batteryFrom(settings: Readonly<Record<string, unknown>>): number | undefined {
  for (const key of ['batteryLevel', 'battery', 'batteryPercent']) {
    const v = settings[key];
    if (typeof v === 'number' && v >= 0 && v <= 100) return v;
  }
  return undefined;
}

export function useTether(client: ApiClient | null, sessionId: string | null): TetherState {
  const source = useMemo(() => createCaptureSource(), []);
  const availability = useMemo(() => getTetherAvailability(), []);

  const [camera, setCamera] = useState<CameraInfo | null>(null);
  const [battery, setBattery] = useState<number | undefined>(undefined);
  const [items, setItems] = useState<Map<string, TetherShot>>(new Map());
  const [error, setError] = useState<string | null>(null);

  // Máy ảnh hiện tại đọc từ trong handler sự kiện. Giữ trong ref chứ không chỉ
  // trong state: nếu đọc từ state, closure của handler sẽ bắt giá trị lúc đăng
  // ký và mãi mãi thấy `null`.
  const cameraRef = useRef<CameraInfo | null>(null);

  /**
   * Id máy ảnh do MÁY CHỦ cấp, khác với id của phiên kết nối.
   *
   * Id từ SDK chỉ ổn định trong một phiên (xem `CameraInfo.id` trong
   * capture/types.ts), nên gắn nó vào ảnh là gắn một thứ vô nghĩa với lần cắm
   * sau. Máy chủ cấp id bền và từ chối ảnh trỏ tới máy ảnh của người khác.
   */
  const serverCameraID = useRef<string>('');
  const setCurrentCamera = useCallback((info: CameraInfo | null) => {
    cameraRef.current = info;
    setCamera(info);
    if (!info) {
      setBattery(undefined);
      serverCameraID.current = '';
    }
  }, []);

  // --- hàng đợi đẩy metadata -------------------------------------------------

  const pending = useRef<Map<string, ImageInput>>(new Map());
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const inFlight = useRef(false);
  const flushRef = useRef<() => void>(() => {});

  const schedule = useCallback((delay: number) => {
    if (timer.current) return;
    timer.current = setTimeout(() => {
      timer.current = null;
      flushRef.current();
    }, delay);
  }, []);

  const flush = useCallback(async () => {
    if (inFlight.current || !client || !sessionId) return;

    const batch = [...pending.current.values()].slice(0, MAX_BATCH);
    if (batch.length === 0) return;

    inFlight.current = true;
    try {
      await client.batchImages(sessionId, batch);
      for (const input of batch) pending.current.delete(input.clientId);
    } catch {
      // Giữ nguyên hàng đợi và thử lại. KHÔNG hiện lỗi cho người dùng: ảnh vẫn
      // nằm an toàn trên thẻ, và một thông báo đỏ giữa buổi chụp vì mạng chập
      // chờn chỉ làm người ta hoảng chứ không giúp được gì.
    } finally {
      inFlight.current = false;
      if (pending.current.size > 0) schedule(RETRY_DELAY_MS);
    }
  }, [client, sessionId, schedule]);

  flushRef.current = () => void flush();

  // --- nhận sự kiện ----------------------------------------------------------

  // Hẹn giờ tự lấy preview, theo từng item, để huỷ được khi preview tự về.
  const previewTimers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());

  const upsert = useCallback((item: CaptureItemRef, previewUri: string | null) => {
    setItems(prev => {
      const next = new Map(prev);
      const before = next.get(item.id);
      next.set(item.id, {
        clientId: item.id,
        filename: item.filename,
        capturedAt: item.capturedAt,
        // Sự kiện sau không được xoá preview đã có: native bắn cùng một item
        // nhiều lần, và lần sau đôi khi không kèm ảnh.
        previewUri: previewUri ?? before?.previewUri ?? null,
      });
      return next;
    });
  }, []);

  useEffect(() => {
    if (!source) return;

    return source.subscribe(event => {
      switch (event.type) {
        case 'cameraConnected':
          setCurrentCamera(event.camera);
          setError(null);
          break;

        case 'cameraDisconnected':
          setCurrentCamera(null);
          // Rớt kết nối PHẢI hiện ra ngay: mỗi giây người dùng không biết là một
          // tấm ảnh tưởng đã về mà thật ra chưa.
          setError(event.reason);
          break;

        case 'itemCaptured': {
          const preview = event.preview?.uri ?? null;
          upsert(event.item, preview);

          if (preview) {
            const t = previewTimers.current.get(event.item.id);
            if (t) {
              clearTimeout(t);
              previewTimers.current.delete(event.item.id);
            }
          } else if (!previewTimers.current.has(event.item.id)) {
            const itemId = event.item.id;
            previewTimers.current.set(
              itemId,
              setTimeout(() => {
                previewTimers.current.delete(itemId);
                const cam = cameraRef.current;
                // KHÔNG tự lấy preview khi máy ảnh bắt buộc tải cả file RAW:
                // với 55MB một tấm, làm thế là tự dựng hàng đợi tải hàng chục
                // gigabyte mà người dùng không hề yêu cầu.
                if (!cam || !can(cam, 'previewWithoutFullDownload')) return;
                source
                  .fetchPreview(cam.id, itemId)
                  .then(handle =>
                    setItems(prev => {
                      const before = prev.get(itemId);
                      if (!before) return prev;
                      const next = new Map(prev);
                      next.set(itemId, { ...before, previewUri: handle.uri });
                      return next;
                    }),
                  )
                  .catch(() => {
                    // Thiếu một preview không phải sự cố của cả buổi chụp: ô đó
                    // ở trạng thái trống, những tấm khác vẫn chảy về bình thường.
                  });
              }, PREVIEW_FALLBACK_MS),
            );
          }

          // Đẩy metadata NGAY khi biết tới tấm ảnh, không chờ preview. Preview
          // là chuyện hiển thị; danh sách ảnh của buổi chụp là chuyện dữ liệu,
          // và hai thứ đó không được phụ thuộc nhau.
          pending.current.set(event.item.id, toImageInput(event.item, serverCameraID.current));
          schedule(BATCH_DELAY_MS);
          break;
        }

        case 'settingsChanged': {
          const level = batteryFrom(event.changed);
          if (level !== undefined) setBattery(level);
          break;
        }

        case 'error':
          setError(event.message);
          break;

        default:
          // `transferProgress` và `liveViewFrame` có người nhận riêng.
          break;
      }
    });
  }, [source, schedule, setCurrentCamera, upsert]);

  // --- tìm và kết nối máy ảnh ------------------------------------------------

  useEffect(() => {
    if (!source || !sessionId) return;

    let cancelled = false;
    let connecting = false;

    const stopDiscovery = source.startDiscovery(found => {
      // Phase 1 chỉ làm việc với MỘT máy ảnh: nối ngay máy đầu tiên tìm thấy.
      // Người dùng cắm dây rồi mở app là đã nói rõ ý định, bắt họ bấm thêm một
      // nút "kết nối" chỉ thêm một bước giữa lúc đang chụp.
      if (cancelled || connecting || cameraRef.current) return;
      connecting = true;

      source
        .connect(found.id)
        .then(async info => {
          if (cancelled) {
            await source.disconnect(info.id).catch(() => {});
            return;
          }
          setCurrentCamera(info);
          setError(null);

          // Ghi nhận thân máy để ảnh biết mình từ máy nào. Hỏng thì KHÔNG chặn
          // tether: ảnh vẫn phải chảy về: thiếu id máy ảnh chỉ làm mất một
          // thông tin phụ, còn chặn là mất cả buổi chụp.
          if (client) {
            try {
              const registered = await client.registerCamera({
                manufacturer: info.manufacturer,
                model: info.model,
                firmware: info.firmwareVersion,
                transport: info.transport,
                capabilities: [...info.capabilities],
              });
              if (!cancelled) serverCameraID.current = registered.ID;
            } catch {
              // Không hiện lỗi: người dùng không làm gì được với nó.
            }
          }

          if (can(info, 'settingsRead')) {
            try {
              setBattery(batteryFrom(await source.readSettings(info.id)));
            } catch {
              // Không đọc được thông số thì ẩn mức pin. Tether vẫn chạy — đây
              // là thông tin phụ, không phải điều kiện để nhận ảnh.
            }
          }
        })
        .catch((e: unknown) => {
          if (!cancelled) setError(e instanceof Error ? e.message : String(e));
        })
        .finally(() => {
          connecting = false;
        });
    });

    return () => {
      cancelled = true;
      stopDiscovery();
      const info = cameraRef.current;
      if (info) {
        // Ngắt kết nối khi rời buổi chụp. Giữ phiên mở sẽ khoá máy ảnh lại với
        // mọi ứng dụng khác, kể cả sau khi người dùng đã đóng màn hình này.
        void source.disconnect(info.id).catch(() => {});
        cameraRef.current = null;
      }
    };
  }, [source, sessionId, client, setCurrentCamera]);

  // Đổi buổi chụp thì xoá sạch ảnh của buổi trước, kể cả hàng đợi chưa đẩy: gán
  // chúng vào buổi mới sẽ làm hỏng dữ liệu của cả hai.
  useEffect(() => {
    setItems(new Map());
    setCamera(null);
    setBattery(undefined);
    setError(null);
    pending.current.clear();
  }, [sessionId]);

  // Dọn mọi hẹn giờ khi rời màn hình.
  useEffect(() => {
    const timers = previewTimers.current;
    return () => {
      if (timer.current) clearTimeout(timer.current);
      for (const t of timers.values()) clearTimeout(t);
      timers.clear();
    };
  }, []);

  const shots = useMemo(
    () =>
      [...items.values()].sort((a, b) =>
        // Mới nhất lên đầu, giống lưới ảnh đã đồng bộ.
        b.capturedAt.localeCompare(a.capturedAt),
      ),
    [items],
  );

  const previews = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of items.values()) if (s.previewUri) map.set(s.clientId, s.previewUri);
    return map;
  }, [items]);

  const cameraView: CameraView | null = useMemo(
    () => (camera ? { model: camera.model, transport: camera.transport, battery } : null),
    [camera, battery],
  );

  return {
    availability,
    camera: cameraView,
    shots,
    previews,
    previewNeedsFullDownload: camera ? !can(camera, 'previewWithoutFullDownload') : false,
    error,
  };
}
