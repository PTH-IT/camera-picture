/**
 * Hợp đồng capture — ranh giới quan trọng nhất của dự án.
 *
 * Hôm nay chỉ có một implementation: iOS + CascableCore.
 * Sau này sẽ có Android + libgphoto2 (NDK), và hai bên KHÔNG có cùng khả năng.
 *
 * Vì vậy file này tuân thủ ba luật. Vi phạm bất kỳ luật nào sẽ khiến việc thêm
 * Android trở thành viết lại app, chứ không phải thêm một implementation:
 *
 *   1. KHÔNG rẽ nhánh theo hãng máy hay theo SDK. Rẽ nhánh theo `capabilities`.
 *      "Nikon làm được X" là sai — đúng phải là "đường capture này báo là làm được X",
 *      và điều đó khác nhau giữa CascableCore, libgphoto2, và giữa từng body Nikon.
 *
 *   2. KHÔNG bao giờ để pixel đi qua cầu JS. Ảnh luôn được tham chiếu bằng
 *      `ImageHandle` (URI phía native). Một NEF là 50–60MB; đẩy nó qua JS là
 *      giết hiệu năng và làm OOM app.
 *
 *   3. Duyệt thẻ và tải file là HAI việc khác nhau. Một đám cưới là 100GB+ NEF —
 *      điện thoại không chứa nổi. Luồng đúng là duyệt thẻ → chỉ lấy preview →
 *      chỉ tải nguyên bản cho ảnh đã chọn.
 */

/**
 * Khả năng của một đường capture cụ thể, với một body cụ thể, qua một transport
 * cụ thể. Không suy ra từ tên hãng — luôn đọc từ `CameraInfo.capabilities`.
 */
export type CameraCapability =
  /** Bấm chụp từ app. */
  | 'remoteShutter'
  /** Stream khung ngắm. CascableCore có với Nikon; libgphoto2 thì không. */
  | 'liveView'
  /** Đọc được thông số (ISO, khẩu, tốc). */
  | 'settingsRead'
  /** Ghi được thông số. Thường hẹp hơn đọc rất nhiều. */
  | 'settingsWrite'
  /** Bắn sự kiện khi người dùng bấm máy trên thân máy (tethered shooting). */
  | 'tetheredEvents'
  /** Liệt kê được file trên thẻ mà không phải tải về. */
  | 'storageBrowse'
  /**
   * Lấy được JPEG preview nhúng mà KHÔNG phải tải toàn bộ file RAW.
   *
   * Đây là giả định ngầm nguy hiểm nhất trong toàn bộ kiến trúc, và là mục #5
   * trong danh sách spike. Nếu cờ này false, chiến lược "để ảnh trên thẻ" sụp đổ
   * và phải thiết kế lại — nên nó là capability, không phải điều mặc nhiên.
   */
  | 'previewWithoutFullDownload'
  /** Quay video. */
  | 'videoRecord';

export type Transport = 'usb' | 'wifi';

export interface CameraInfo {
  /** Định danh ổn định trong một phiên kết nối. Không phải serial number. */
  readonly id: string;
  readonly manufacturer: string;
  readonly model: string;
  readonly firmwareVersion?: string;
  readonly transport: Transport;
  /**
   * Nguồn sự thật duy nhất để quyết định hiện/ẩn tính năng trong UI.
   * Hai body Nikon cùng đời có thể khác nhau ở đây.
   */
  readonly capabilities: readonly CameraCapability[];
}

export function can(camera: CameraInfo, capability: CameraCapability): boolean {
  return camera.capabilities.includes(capability);
}

/** Định dạng file đã biết. `unknown` là hợp lệ — đừng crash vì một đuôi lạ. */
export type ImageFormat = 'NEF' | 'NRW' | 'JPEG' | 'HEIF' | 'TIFF' | 'MOV' | 'MP4' | 'unknown';

/**
 * Một mục trên thẻ nhớ. Cố ý KHÔNG chứa pixel — đây chỉ là metadata thu được
 * từ việc duyệt thẻ, rẻ và nhanh, dùng để dựng lưới ảnh trước khi tải bất cứ gì.
 */
export interface CaptureItemRef {
  readonly id: string;
  readonly filename: string;
  readonly format: ImageFormat;
  readonly byteSize: number;
  /** ISO 8601. Giờ của máy ảnh, có thể lệch giờ điện thoại — đừng giả định. */
  readonly capturedAt: string;
  readonly isRaw: boolean;
  readonly hasEmbeddedPreview: boolean;
}

/**
 * Tham chiếu tới pixel do phía native giữ. `uri` trỏ tới file/asset native,
 * dùng trực tiếp được với Skia. Bytes KHÔNG bao giờ đi qua JS.
 */
export interface ImageHandle {
  readonly uri: string;
  readonly width: number;
  readonly height: number;
  readonly byteSize: number;
}

export type CaptureEvent =
  | { type: 'cameraConnected'; camera: CameraInfo }
  | { type: 'cameraDisconnected'; cameraId: string; reason: string }
  /**
   * Người dùng vừa bấm máy. `preview` có thể chưa sẵn — đừng chặn UI chờ nó.
   * Hiện placeholder ngay, điền ảnh khi `preview` tới.
   */
  | { type: 'itemCaptured'; item: CaptureItemRef; preview?: ImageHandle }
  | { type: 'transferProgress'; itemId: string; bytesTransferred: number; bytesTotal: number }
  | { type: 'liveViewFrame'; handle: ImageHandle }
  | { type: 'settingsChanged'; changed: Readonly<Record<string, unknown>> }
  | { type: 'error'; code: CaptureErrorCode; message: string; itemId?: string };

/**
 * Mã lỗi ổn định, để UI xử lý được mà không phải parse chuỗi tiếng Anh từ SDK.
 * Mỗi implementation có trách nhiệm ánh xạ lỗi riêng của nó về đây.
 */
export type CaptureErrorCode =
  | 'permissionDenied'
  | 'cameraBusy'
  | 'connectionLost'
  | 'unsupportedOperation'
  | 'storageReadFailed'
  | 'transferFailed'
  | 'licenseInvalid'
  | 'unknown';

export class CaptureError extends Error {
  constructor(
    readonly code: CaptureErrorCode,
    message: string,
    readonly itemId?: string,
  ) {
    super(message);
    this.name = 'CaptureError';
  }
}

/**
 * Hợp đồng mà mọi đường capture phải thoả mãn.
 *
 * iOS  → CascableCore  (phase 1)
 * Android → libgphoto2 NDK (phase sau)
 *
 * Tầng trên của app chỉ được biết interface này. Nếu ở đâu đó trong app xuất hiện
 * chữ "Cascable", đó là rò rỉ trừu tượng và sẽ trở thành nợ khi làm Android.
 */
export interface CaptureSource {
  /** Bắt đầu tìm máy ảnh. Trả về hàm huỷ. */
  startDiscovery(onFound: (camera: CameraInfo) => void): () => void;

  connect(cameraId: string): Promise<CameraInfo>;
  disconnect(cameraId: string): Promise<void>;

  /** Đăng ký nhận sự kiện. Trả về hàm huỷ đăng ký. */
  subscribe(listener: (event: CaptureEvent) => void): () => void;

  /**
   * Duyệt thẻ nhớ. KHÔNG tải file.
   * Phân trang vì một buổi chụp có thể vài nghìn tấm.
   */
  listItems(cameraId: string, opts?: { after?: string; limit?: number }): Promise<CaptureItemRef[]>;

  /**
   * Lấy preview để hiển thị.
   *
   * Nếu camera báo `previewWithoutFullDownload`, đây là thao tác rẻ (vài trăm KB).
   * Nếu không, implementation buộc phải tải cả file — khi đó UI phải cảnh báo
   * người dùng trước, vì 2000 tấm × 55MB sẽ không bao giờ xong.
   */
  fetchPreview(cameraId: string, itemId: string): Promise<ImageHandle>;

  /** Tải nguyên bản. Chỉ gọi cho ảnh người dùng đã chọn. */
  fetchOriginal(
    cameraId: string,
    itemId: string,
    opts?: { onProgress?: (bytesTransferred: number, bytesTotal: number) => void },
  ): Promise<ImageHandle>;

  /** Yêu cầu `remoteShutter`. Kiểm tra bằng `can()` trước khi gọi. */
  triggerShutter(cameraId: string): Promise<CaptureItemRef>;

  /** Yêu cầu `liveView`. Frame tới qua sự kiện `liveViewFrame`. */
  startLiveView(cameraId: string): Promise<void>;
  stopLiveView(cameraId: string): Promise<void>;

  readSettings(cameraId: string): Promise<Readonly<Record<string, unknown>>>;
  writeSetting(cameraId: string, key: string, value: unknown): Promise<void>;
}
