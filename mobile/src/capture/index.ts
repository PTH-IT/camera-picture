import { NativeEventEmitter, Platform } from 'react-native';
import NativeCaptureSource from './NativeCaptureSource';
import { createCaptureSourceFrom, type NativeEventTap } from './adapter';
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
 * Tên kênh sự kiện. Phải khớp `supportedEvents()` trong
 * ios/CaptureSource/CaptureSourceModule.swift — không có codegen nối hai bên,
 * và sai một chữ ở đây làm mọi sự kiện im lặng biến mất, không có lỗi nào.
 */
const EVENT_NAME = 'captureEvent';

let instance: CaptureSource | null = null;

/**
 * Trả về `CaptureSource`, hoặc `null` nếu nền tảng chưa hỗ trợ tether.
 *
 * Cố ý trả `null` thay vì ném lỗi: "Android chưa có tether" là trạng thái sản phẩm
 * bình thường trong phase 1, không phải sự cố. UI kiểm tra null và ẩn phần tether,
 * phần xem/chỉnh/AI vẫn chạy đầy đủ.
 *
 * Kết quả được nhớ lại và dùng chung cho cả app. Đây KHÔNG phải tối ưu tốc độ:
 * adapter đếm số bên đang tìm máy ảnh để biết khi nào gọi `stopDiscovery`, và
 * hai adapter song song sẽ đếm riêng — màn hình này đóng lại làm tắt discovery
 * của màn hình kia, tại đúng lúc người dùng đang chờ máy ảnh hiện ra.
 */
export function createCaptureSource(): CaptureSource | null {
  if (!NativeCaptureSource) return null;
  if (instance) return instance;

  // NativeEventEmitter nhận một NativeModule cũ; Turbo Module có `addListener`
  // và `removeListeners` nên chạy đúng, chỉ là kiểu không khớp. Ép kiểu ngay tại
  // đây, ở đúng một dòng, thay vì nới lỏng kiểu của cả tầng capture.
  const emitter = new NativeEventEmitter(
    NativeCaptureSource as unknown as ConstructorParameters<typeof NativeEventEmitter>[0],
  );

  const tap: NativeEventTap = onEvent => {
    const sub = emitter.addListener(EVENT_NAME, onEvent);
    return () => sub.remove();
  };

  instance = createCaptureSourceFrom(NativeCaptureSource, tap);
  return instance;
}
