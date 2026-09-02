import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ApiClient,
  ApiError,
  pullAllChanges,
  type EditRecord,
  type ImageRecord,
  type SessionSummary,
} from '../api/client';
import type { StorageOption, StorageProvider, StorageUsage, User } from '../account/types';

/**
 * Tầng trạng thái nối API vào màn hình.
 *
 * Màn hình được giữ THUẦN TRÌNH BÀY: chúng nhận dữ liệu qua props và không biết
 * gì về mạng. Nhờ vậy bản xem trước trong trình duyệt truyền dữ liệu mẫu vào
 * đúng những màn hình đó, và thứ nhìn thấy khi review chính là thứ chạy thật —
 * không phải một bản sao có thể trôi lệch theo thời gian.
 */

/** Nhịp kéo delta khi đang mở một buổi chụp. */
const SYNC_INTERVAL_MS = 5_000;

export interface AsyncState<T> {
  data: T | null;
  loading: boolean;
  error: ApiError | null;
}

function useAsync<T>(fn: () => Promise<T>, deps: unknown[]): AsyncState<T> & { reload: () => void } {
  const [state, setState] = useState<AsyncState<T>>({ data: null, loading: true, error: null });
  const [tick, setTick] = useState(0);

  // Giữ tham chiếu hàm trong ref để không phải đưa nó vào deps — nếu đưa vào,
  // mọi lần render lại của component cha sẽ kích hoạt một lượt tải mới.
  const fnRef = useRef(fn);
  fnRef.current = fn;

  useEffect(() => {
    let cancelled = false;
    setState(s => ({ ...s, loading: true }));

    fnRef
      .current()
      .then(data => {
        // Bỏ qua kết quả nếu component đã unmount hoặc deps đã đổi: ghi vào state
        // sau khi unmount là rò rỉ, và ghi kết quả cũ đè kết quả mới là lỗi hiển
        // thị dữ liệu sai mà rất khó tái hiện.
        if (!cancelled) setState({ data, loading: false, error: null });
      })
      .catch(e => {
        if (!cancelled) {
          setState({
            data: null,
            loading: false,
            error: e instanceof ApiError ? e : new ApiError('internal', String(e)),
          });
        }
      });

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick]);

  return { ...state, reload: useCallback(() => setTick(t => t + 1), []) };
}

export function useMe(client: ApiClient | null): AsyncState<User> {
  return useAsync(async () => {
    if (!client) throw new ApiError('unauthorized', 'chưa đăng nhập');
    return client.me();
  }, [client]);
}

export function useStorage(client: ApiClient | null) {
  const options = useAsync(async () => {
    if (!client) throw new ApiError('unauthorized', 'chưa đăng nhập');
    return client.storageOptions();
  }, [client]);

  const usage = useAsync(async () => {
    if (!client) throw new ApiError('unauthorized', 'chưa đăng nhập');
    return client.storageUsage();
  }, [client]);

  const select = useCallback(
    async (p: StorageProvider) => {
      if (!client) return;
      await client.selectStorage(p);
      options.reload();
      usage.reload();
    },
    [client, options, usage],
  );

  return {
    options: options.data?.options ?? ([] as StorageOption[]),
    selected: options.data?.selected ?? ('device' as StorageProvider),
    usage: usage.data as StorageUsage | null,
    loading: options.loading || usage.loading,
    // Drive chưa cấu hình trên máy chủ trả 501; đó KHÔNG phải lỗi cần hiện cho
    // người dùng, chỉ là tính năng chưa bật.
    error: options.error?.isNotConfigured ? null : options.error,
    select,
  };
}

/**
 * Đồng bộ delta cho một buổi chụp.
 *
 * Giữ con trỏ revision và gộp các thay đổi vào bản đồ cục bộ. Đây là chỗ DUY
 * NHẤT trong app biết về con trỏ — rải logic này ra nhiều màn hình là cách chắc
 * chắn để hai màn hình lệch con trỏ nhau và một trong hai bỏ sót ảnh.
 */
export function useSessionSync(client: ApiClient | null, sessionId: string | null) {
  const [images, setImages] = useState<Map<string, ImageRecord>>(new Map());
  const [edits, setEdits] = useState<Map<string, EditRecord>>(new Map());
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  // Con trỏ giữ trong ref chứ không chỉ trong state: hàm sync đọc nó, và nếu đọc
  // từ state thì closure sẽ bắt giá trị cũ và kéo lại cùng một trang mãi mãi.
  const cursor = useRef(0);

  const sync = useCallback(async () => {
    if (!client || !sessionId) return;
    setLoading(true);
    try {
      const page = await pullAllChanges(client, sessionId, cursor.current);
      cursor.current = page.revision;
      setRevision(page.revision);

      if (page.images.length > 0) {
        setImages(prev => {
          const next = new Map(prev);
          for (const img of page.images) {
            // Ảnh đã xoá vẫn phải xử lý, không bỏ qua: đó là cách client biết
            // gỡ nó khỏi danh sách.
            if (img.deleted) next.delete(img.id);
            else next.set(img.id, img);
          }
          return next;
        });
      }
      if (page.edits.length > 0) {
        setEdits(prev => {
          const next = new Map(prev);
          for (const e of page.edits) next.set(e.imageId, e);
          return next;
        });
      }
      setError(null);
    } catch (e) {
      setError(e instanceof ApiError ? e : new ApiError('internal', String(e)));
    } finally {
      setLoading(false);
    }
  }, [client, sessionId]);

  // Đổi buổi chụp thì phải xoá sạch trạng thái cũ, kể cả con trỏ. Giữ lại con
  // trỏ của buổi trước sẽ khiến buổi mới không kéo được gì cả.
  useEffect(() => {
    cursor.current = 0;
    setImages(new Map());
    setEdits(new Map());
    setRevision(0);
  }, [sessionId]);

  useEffect(() => {
    void sync();
  }, [sync]);

  /**
   * Kéo delta định kỳ trong lúc buổi chụp đang mở.
   *
   * Không có vòng lặp này, client chỉ đồng bộ đúng MỘT lần lúc mở buổi chụp.
   * Hai hậu quả, cái sau tệ hơn cái trước:
   *
   *  - Ảnh do thiết bị khác đẩy lên (iPad của trợ lý) không bao giờ hiện ra,
   *    dù giao thức delta sinh ra chính là để làm việc đó.
   *  - Ảnh của chính máy này, sau khi đẩy metadata lên, vẫn nằm ở trạng thái
   *    "máy chủ chưa biết" mãi mãi. Màn hình chỉnh màu chỉ làm việc được với id
   *    do máy chủ cấp, nên nó sẽ trống trơn trong khi lưới ảnh đầy ắp.
   *
   * 5 giây là mức đủ nhanh để không ai để ý, và vẫn rẻ: mỗi lần chỉ là một
   * truy vấn theo con trỏ revision, thường trả về rỗng.
   */
  useEffect(() => {
    if (!client || !sessionId) return;
    const timer = setInterval(() => void sync(), SYNC_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [client, sessionId, sync]);

  /**
   * Cập nhật lạc quan cho chỉnh sửa.
   *
   * Người dùng chấm sao khi đang lướt hàng trăm ảnh; chờ mạng phản hồi mới đổi
   * giao diện là trải nghiệm chậm rõ rệt. Ghi vào state ngay, và nếu server từ
   * chối thì hoàn tác — thay vì để người dùng tưởng đã lưu.
   */
  const putEdit = useCallback(
    async (imageId: string, patch: Partial<EditRecord>) => {
      if (!client) return;
      const before = edits.get(imageId);
      const merged: EditRecord = {
        imageId,
        rating: patch.rating ?? before?.rating ?? 0,
        flagged: patch.flagged ?? before?.flagged ?? false,
        rejected: patch.rejected ?? before?.rejected ?? false,
        presetId: patch.presetId ?? before?.presetId,
        overrides: patch.overrides ?? before?.overrides,
        revision: before?.revision ?? 0,
        updatedAt: new Date().toISOString(),
      };
      setEdits(prev => new Map(prev).set(imageId, merged));

      try {
        const saved = await client.putEdit(imageId, {
          rating: merged.rating,
          flagged: merged.flagged,
          rejected: merged.rejected,
          presetId: merged.presetId,
          overrides: merged.overrides,
        });
        setEdits(prev => new Map(prev).set(imageId, saved));
        cursor.current = Math.max(cursor.current, saved.revision);
        setRevision(cursor.current);
      } catch (e) {
        setEdits(prev => {
          const next = new Map(prev);
          if (before) next.set(imageId, before);
          else next.delete(imageId);
          return next;
        });
        setError(e instanceof ApiError ? e : new ApiError('internal', String(e)));
      }
    },
    [client, edits],
  );

  const list = useMemo(
    () =>
      [...images.values()].sort((a, b) =>
        // Mới nhất lên đầu: trong lúc chụp, tấm vừa vào là tấm người dùng muốn thấy.
        b.capturedAt.localeCompare(a.capturedAt),
      ),
    [images],
  );

  return { images: list, edits, revision, loading, error, sync, putEdit };
}

export function useSessions(
  client: ApiClient | null,
): AsyncState<SessionSummary[]> & { reload: () => void } {
  return useAsync(async () => {
    if (!client) throw new ApiError('unauthorized', 'chưa đăng nhập');
    return client.listSessions();
  }, [client]);
}
