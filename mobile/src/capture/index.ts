import { Platform } from 'react-native';
import NativeCaptureSource from './NativeCaptureSource';
import type { CaptureSource } from './types';

export * from './types';

/**
 * Trạng thái tether của nền tảng hiện tại.
 *
 * Đây là hệ quả trực tiếp của quyết định (a) rồi (b): iOS có tether ngay,
 * Android phase 1 chỉ xem/chỉnh ảnh đã đồng bộ, tether đến sau qua libgphoto2.
 *
 * Việc mô hình hoá tường minh — thay vì để `NativeCaptureSource` là null và
 * hy vọng không ai gọi — buộc UI phải xử lý trường hợp "không tether được"
 * ngay từ ngày đầu. Nếu để đến lúc làm Android mới nghĩ tới, mọi màn hình
 * liên quan tới máy ảnh sẽ phải sửa lại.
 */
export type TetherAvailability =
  | { available: true }
  | { available: false; reason: 'platformNotSupportedYet' | 'nativeModuleMissing' };

export function getTetherAvailability(): TetherAvailability {
  if (NativeCaptureSource) return { available: true };

  // Android phase 1: chủ ý chưa có. Không phải lỗi cấu hình.
  if (Platform.OS === 'android') {
    return { available: false, reason: 'platformNotSupportedYet' };
  }

  // iOS mà thiếu module là lỗi build thật — pod chưa cài, codegen chưa chạy.
  return { available: false, reason: 'nativeModuleMissing' };
}

/**
 * Trả về `CaptureSource`, hoặc `null` nếu nền tảng chưa hỗ trợ tether.
 *
 * Cố ý trả `null` thay vì ném lỗi: "Android chưa có tether" là trạng thái sản phẩm
 * bình thường trong phase 1, không phải sự cố. UI kiểm tra null và ẩn phần tether,
 * phần xem/chỉnh/AI vẫn chạy đầy đủ.
 */
export function createCaptureSource(): CaptureSource | null {
  if (!NativeCaptureSource) return null;

  // TODO(phase-1): implement adapter đóng/mở gói giữa Spec (kiểu nghèo, do
  // codegen ép) và CaptureSource (kiểu giàu). Xem chú thích trong
  // NativeCaptureSource.ts về lý do phải tách hai tầng này.
  throw new Error('createCaptureSource: adapter chưa được implement');
}
