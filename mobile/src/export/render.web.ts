import type { ColorAdjustments } from '../color/adjustments';
import type { PresetVisual } from '../color/GradedImageProps';
import type { RenderedImage } from './render';

/**
 * Bản cho trình duyệt: KHÔNG kết xuất.
 *
 * Bản xem trước dùng CSS filter để xấp xỉ màu, nên nếu để nó xuất file thì file
 * đó sẽ khác hẳn thứ máy thật tạo ra — và không có gì trên file nhắc rằng nó là
 * bản xấp xỉ. Ném lỗi rõ ràng còn hơn giao một tấm ảnh sai màu.
 */
export function renderGraded(
  _uri: string,
  _preset: PresetVisual | null | undefined,
  _amount: number,
  _adjustments?: ColorAdjustments,
  _quality?: number,
): Promise<RenderedImage> {
  return Promise.reject(new Error('Xuất ảnh chỉ chạy trên máy thật, không chạy ở bản xem trước.'));
}

export type { RenderedImage } from './render';
