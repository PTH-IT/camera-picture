import type { EditRecord, ImageRecord } from '../api/client';
import type { PresetVisual } from '../color/GradedImageProps';
import type { StorageProvider } from '../account/types';

/**
 * Mô hình hiển thị mà các màn hình nhận vào.
 *
 * Màn hình được giữ THUẦN TRÌNH BÀY và không biết gì về mạng. Nhờ vậy bản xem
 * trước trong trình duyệt truyền dữ liệu mẫu vào đúng những màn hình chạy thật —
 * thứ nhìn thấy khi review chính là thứ người dùng sẽ thấy, không phải một bản
 * sao có thể trôi lệch theo thời gian.
 */

export interface ShotView {
  id: string;
  filename: string;
  /** URI ảnh để hiển thị. Với RAW là JPEG preview nhúng, không phải bản decode. */
  uri: string;
  /** Giờ đã định dạng để hiển thị. Giờ MÁY ẢNH, có thể lệch giờ điện thoại. */
  time: string;
  iso?: number;
  aperture?: string;
  shutter?: string;
  focal?: string;
  rating: number;
  flagged: boolean;
  rejected: boolean;
  /** Bản gốc đã lên máy chủ chưa. `false` là trạng thái BÌNH THƯỜNG — phần lớn
   *  ảnh nằm trên thẻ suốt buổi chụp. Xem docs/adr/0001-capture-strategy.md. */
  originalUploaded: boolean;
}

export interface SessionView {
  id: string;
  name: string;
  client: string;
  date: string;
  shots: number;
  live: boolean;
}

export interface CameraView {
  model: string;
  transport: 'usb' | 'wifi';
  /**
   * Có thể KHÔNG có. Mức pin đọc từ bảng thông số của máy ảnh, mà không phải
   * đường capture nào cũng cho đọc thông số — hợp đồng capture khai `settingsRead`
   * là một khả năng riêng. Bịa ra một con số khi không đọc được còn tệ hơn không
   * hiện gì: người dùng sẽ tin nó.
   */
  battery?: number;
}

export type PresetView = PresetVisual;

export interface StorageOptionView {
  provider: StorageProvider;
  capabilities: readonly string[];
  warning?: string;
}

/**
 * Chuyển bản ghi từ API sang mô hình hiển thị.
 *
 * Đặt ở một chỗ để mọi màn hình hiểu dữ liệu giống nhau — nếu mỗi màn hình tự
 * diễn giải, hai màn hình sẽ hiển thị cùng một ảnh theo hai cách khác nhau.
 */
export function toShotView(
  img: ImageRecord,
  edit: EditRecord | undefined,
  previewUri: string,
): ShotView {
  const exif = (img as { exif?: Record<string, unknown> }).exif ?? {};
  return {
    id: img.id,
    filename: img.filename,
    uri: previewUri,
    time: formatTime(img.capturedAt),
    iso: numberOrUndefined(exif.iso),
    aperture: stringOrUndefined(exif.aperture),
    shutter: stringOrUndefined(exif.shutter),
    focal: stringOrUndefined(exif.focal),
    rating: edit?.rating ?? 0,
    flagged: edit?.flagged ?? false,
    rejected: edit?.rejected ?? false,
    // Có bản gốc trên máy chủ nghĩa là asset tier `original` tồn tại. Không có
    // là bình thường, không phải lỗi đồng bộ.
    originalUploaded: !!img.assets?.original,
  };
}

/** Giờ hiển thị. Dùng chung để lưới ảnh đã đồng bộ và ảnh vừa bấm đọc giống nhau. */
export function formatTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const p = (n: number) => String(n).padStart(2, '0');
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

function numberOrUndefined(v: unknown): number | undefined {
  return typeof v === 'number' ? v : undefined;
}

function stringOrUndefined(v: unknown): string | undefined {
  return typeof v === 'string' ? v : undefined;
}
