/**
 * Client gọi backend.
 *
 * Bản đối ứng phía server: backend/internal/api. Các kiểu dữ liệu phải khớp với
 * backend/internal/protocol — không có codegen giữa hai bên, nên sửa một bên thì
 * phải sửa cả bên kia.
 */

import type {
  AuthErrorCode,
  AuthResult,
  Entitlement,
  OIDCSignInRequest,
  StorageOption,
  StorageProvider,
  StorageUsage,
  User,
} from '../account/types';

// ---------------------------------------------------------------------------
// Kiểu dữ liệu đồng bộ — phản chiếu backend/internal/protocol/sync.go
// ---------------------------------------------------------------------------

export type ImageFormat = 'NEF' | 'NRW' | 'JPEG' | 'HEIF' | 'TIFF' | 'MOV' | 'MP4' | 'unknown';
export type AssetTier = 'thumb' | 'preview' | 'proxy' | 'original' | 'export';

export interface Session {
  ID: string;
  UserID: string;
  Name: string;
  ClientName: string;
  StartedAt: string;
  Revision: number;
}

export interface ImageInput {
  clientId: string;
  filename: string;
  format: ImageFormat;
  byteSize: number;
  capturedAt: string;
  isRaw: boolean;
  cameraId?: string;
  exif?: Record<string, unknown>;
}

export interface AssetRecord {
  storageKey: string;
  byteSize: number;
  width?: number;
  height?: number;
}

export interface ImageRecord {
  id: string;
  clientId: string;
  filename: string;
  format: ImageFormat;
  byteSize: number;
  capturedAt: string;
  isRaw: boolean;
  /** Có thể RỖNG, và đó là trạng thái bình thường: ảnh vẫn nằm trên thẻ nhớ. */
  assets?: Partial<Record<AssetTier, AssetRecord>>;
  revision: number;
  deleted: boolean;
  updatedAt: string;
}

export interface EditRecord {
  imageId: string;
  presetId?: string;
  overrides?: Record<string, unknown>;
  rating: number;
  flagged: boolean;
  rejected: boolean;
  revision: number;
  updatedAt: string;
  updatedByDevice?: string;
}

export interface BatchImagesResponse {
  ids: Record<string, string>;
  created: number;
  updated: number;
  revision: number;
}

export interface ChangesResponse {
  images: ImageRecord[];
  edits: EditRecord[];
  revision: number;
  hasMore: boolean;
}

export interface PutEditRequest {
  presetId?: string;
  overrides?: Record<string, unknown>;
  rating: number;
  flagged: boolean;
  rejected: boolean;
  deviceId?: string;
}

export interface ConfirmAssetRequest {
  tier: AssetTier;
  storageKey: string;
  byteSize: number;
  width?: number;
  height?: number;
}

// ---------------------------------------------------------------------------
// Lỗi
// ---------------------------------------------------------------------------

export type ApiErrorCode =
  | AuthErrorCode
  | 'not_found'
  | 'not_configured'
  | 'quota_exceeded'
  | 'not_linked'
  | 'network';

/**
 * Lỗi từ API.
 *
 * `code` là chuỗi ổn định do server cấp — xử lý theo nó, đừng bao giờ so khớp
 * `message`. Message dành cho con người đọc và server được phép đổi bất cứ lúc nào.
 */
export class ApiError extends Error {
  constructor(
    readonly code: ApiErrorCode,
    message: string,
    readonly status: number = 0,
  ) {
    super(message);
    this.name = 'ApiError';
  }

  /** Phiên hết hạn hoặc bị thu hồi — giao diện phải đưa người dùng về đăng nhập. */
  get isUnauthorized(): boolean {
    return this.code === 'unauthorized';
  }

  /** Tính năng chưa được bật trên máy chủ này. Giao diện nên ẩn nút, không báo lỗi. */
  get isNotConfigured(): boolean {
    return this.code === 'not_configured';
  }
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

export interface TokenStore {
  get(): string | null;
  set(token: string | null): void;
}

/**
 * Bộ nhớ token đơn giản trong RAM.
 *
 * Bản dùng thật phải lưu vào Keychain (iOS) hoặc Keystore (Android), KHÔNG phải
 * AsyncStorage: AsyncStorage không được mã hoá, và token phiên ở đó đọc được bởi
 * bất kỳ tiến trình nào truy cập được thư mục của app trên máy đã jailbreak/root.
 */
export function memoryTokenStore(initial: string | null = null): TokenStore {
  let token = initial;
  return {
    get: () => token,
    set: t => {
      token = t;
    },
  };
}

export interface ClientOptions {
  baseUrl: string;
  tokens?: TokenStore;
  /** Tách ra để test được mà không cần mạng thật. */
  fetchImpl?: typeof fetch;
  /** Bị gọi khi server trả 401 — giao diện dùng để đưa về màn hình đăng nhập. */
  onUnauthorized?: () => void;
}

export class ApiClient {
  readonly tokens: TokenStore;
  private readonly baseUrl: string;
  private readonly doFetch: typeof fetch;
  private readonly onUnauthorized?: () => void;

  constructor(opts: ClientOptions) {
    this.baseUrl = opts.baseUrl.replace(/\/+$/, '');
    this.tokens = opts.tokens ?? memoryTokenStore();
    this.doFetch = opts.fetchImpl ?? fetch;
    this.onUnauthorized = opts.onUnauthorized;
  }

  // --- xác thực ---

  async signUp(email: string, password: string, name?: string): Promise<AuthResult> {
    const res = await this.request<AuthResult>('POST', '/v1/auth/signup', {
      email,
      password,
      name,
    });
    this.tokens.set(res.token);
    return res;
  }

  async signIn(email: string, password: string): Promise<AuthResult> {
    const res = await this.request<AuthResult>('POST', '/v1/auth/signin', { email, password });
    this.tokens.set(res.token);
    return res;
  }

  /**
   * Đăng nhập bằng Apple hoặc Google.
   *
   * `nonce` là BẮT BUỘC và phải là chuỗi ngẫu nhiên MỚI cho mỗi lần đăng nhập.
   * Client sinh nonce, gửi bản băm SHA-256 cho Apple/Google, và gửi bản GỐC lên
   * đây. Server từ chối mọi yêu cầu thiếu nonce, nên quên là đăng nhập hỏng ngay
   * chứ không âm thầm mất an toàn.
   */
  async signInWithOIDC(req: OIDCSignInRequest): Promise<AuthResult> {
    const res = await this.request<AuthResult>('POST', '/v1/auth/oidc', req);
    this.tokens.set(res.token);
    return res;
  }

  async signOut(): Promise<void> {
    try {
      await this.request<void>('POST', '/v1/auth/signout');
    } finally {
      // Xoá token cục bộ dù server có trả lỗi gì: người dùng bấm đăng xuất thì
      // phải được đăng xuất, kể cả khi mất mạng.
      this.tokens.set(null);
    }
  }

  async signOutEverywhere(): Promise<void> {
    try {
      await this.request<void>('POST', '/v1/auth/signout-everywhere');
    } finally {
      this.tokens.set(null);
    }
  }

  me(): Promise<User> {
    return this.request<User>('GET', '/v1/me');
  }

  // --- buổi chụp và đồng bộ ---

  createSession(name: string, clientName?: string, startedAt?: Date): Promise<Session> {
    return this.request<Session>('POST', '/v1/sessions', {
      name,
      clientName,
      startedAt: (startedAt ?? new Date()).toISOString(),
    });
  }

  /**
   * Đẩy metadata một lô ảnh.
   *
   * Idempotent theo `clientId`, nên gửi lại lô đã gửi là an toàn — và client
   * PHẢI tận dụng điều đó: buổi chụp thật hay rớt mạng, và cách xử lý đúng là
   * gửi lại mù chứ không cố đoán lần trước đã tới nơi chưa.
   */
  batchImages(sessionId: string, images: ImageInput[]): Promise<BatchImagesResponse> {
    return this.request<BatchImagesResponse>(
      'POST',
      `/v1/sessions/${encodeURIComponent(sessionId)}/images/batch`,
      { images },
    );
  }

  changes(sessionId: string, since: number, limit = 200): Promise<ChangesResponse> {
    const q = `?since=${since}&limit=${limit}`;
    return this.request<ChangesResponse>(
      'GET',
      `/v1/sessions/${encodeURIComponent(sessionId)}/changes${q}`,
    );
  }

  putEdit(imageId: string, edit: PutEditRequest): Promise<EditRecord> {
    return this.request<EditRecord>('PUT', `/v1/images/${encodeURIComponent(imageId)}/edit`, edit);
  }

  confirmAsset(imageId: string, asset: ConfirmAssetRequest): Promise<void> {
    return this.request<void>(
      'POST',
      `/v1/images/${encodeURIComponent(imageId)}/assets/confirm`,
      asset,
    );
  }

  deleteImage(imageId: string): Promise<void> {
    return this.request<void>('DELETE', `/v1/images/${encodeURIComponent(imageId)}`);
  }

  // --- lưu trữ và mua dung lượng ---

  storageOptions(): Promise<{ options: StorageOption[]; selected: StorageProvider }> {
    return this.request('GET', '/v1/storage/options');
  }

  storageUsage(): Promise<StorageUsage> {
    return this.request<StorageUsage>('GET', '/v1/storage/usage');
  }

  selectStorage(provider: StorageProvider): Promise<{ selected: StorageProvider; note: string }> {
    return this.request('POST', '/v1/storage/select', { provider });
  }

  driveAuthUrl(): Promise<{ url: string }> {
    return this.request('GET', '/v1/storage/drive/auth-url');
  }

  linkDrive(code: string): Promise<void> {
    return this.request<void>('POST', '/v1/storage/drive/link', { code });
  }

  redeemPurchase(platform: 'apple' | 'google', receipt: string): Promise<Entitlement> {
    return this.request<Entitlement>('POST', '/v1/billing/redeem', { platform, receipt });
  }

  // --- lõi ---

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = {};
    const token = this.tokens.get();
    if (token) headers.Authorization = `Bearer ${token}`;
    if (body !== undefined) headers['Content-Type'] = 'application/json';

    let res: Response;
    try {
      res = await this.doFetch(`${this.baseUrl}${path}`, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch (e) {
      // Lỗi mạng khác hẳn lỗi từ server và giao diện xử lý khác nhau: mất mạng
      // thì thử lại được, còn 400 thì thử lại bao nhiêu lần cũng vậy.
      throw new ApiError('network', e instanceof Error ? e.message : 'lỗi mạng');
    }

    if (res.status === 401) {
      this.tokens.set(null);
      this.onUnauthorized?.();
    }

    if (!res.ok) {
      let code: ApiErrorCode = 'internal';
      let message = `HTTP ${res.status}`;
      try {
        const parsed = (await res.json()) as { code?: string; message?: string };
        if (parsed.code) code = parsed.code as ApiErrorCode;
        if (parsed.message) message = parsed.message;
      } catch {
        // Server trả thứ không phải JSON (proxy lỗi, gateway timeout). Giữ
        // thông báo mặc định thay vì để lỗi phân tích JSON che mất lỗi thật.
      }
      throw new ApiError(code, message, res.status);
    }

    if (res.status === 204) return undefined as T;

    const text = await res.text();
    if (!text) return undefined as T;
    return JSON.parse(text) as T;
  }
}

/**
 * Kéo hết delta từ một con trỏ.
 *
 * Vòng lặp phân trang nằm ở ĐÂY chứ không rải trong các màn hình. Hợp đồng của
 * server là "gọi lại với since = revision khi hasMore"; viết sai vòng lặp này
 * nghĩa là mất ảnh một cách âm thầm, nên nó chỉ được tồn tại ở một chỗ duy nhất.
 *
 * `maxRounds` là chốt an toàn: nếu server có lỗi khiến con trỏ không tiến, vòng
 * lặp phải dừng và báo thay vì quay mãi và làm nóng máy người dùng.
 */
export async function pullAllChanges(
  client: ApiClient,
  sessionId: string,
  since: number,
  opts: { limit?: number; maxRounds?: number } = {},
): Promise<{ images: ImageRecord[]; edits: EditRecord[]; revision: number; rounds: number }> {
  const limit = opts.limit ?? 200;
  const maxRounds = opts.maxRounds ?? 500;

  const images: ImageRecord[] = [];
  const edits: EditRecord[] = [];
  let cursor = since;
  let rounds = 0;

  for (;;) {
    rounds++;
    if (rounds > maxRounds) {
      throw new ApiError(
        'internal',
        `đồng bộ không hội tụ sau ${maxRounds} vòng — con trỏ có thể không tiến`,
      );
    }

    const page = await client.changes(sessionId, cursor, limit);
    images.push(...page.images);
    edits.push(...page.edits);

    if (page.revision <= cursor && (page.images.length > 0 || page.edits.length > 0)) {
      throw new ApiError('internal', 'con trỏ đồng bộ không tiến nhưng vẫn có bản ghi');
    }
    cursor = page.revision;

    if (!page.hasMore) break;
  }

  return { images, edits, revision: cursor, rounds };
}
