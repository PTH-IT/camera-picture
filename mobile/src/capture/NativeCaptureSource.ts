import type { TurboModule } from 'react-native';
import { TurboModuleRegistry } from 'react-native';

/**
 * Spec Turbo Module cho tầng capture native.
 *
 * Đây KHÔNG phải hợp đồng mà app dùng — app dùng `CaptureSource` trong `types.ts`.
 * File này chỉ là lớp vận chuyển, cố ý nghèo nàn vì codegen của React Native
 * chỉ chấp nhận một tập kiểu hẹp: string, number, boolean, object, array, và
 * Promise của chúng. Không union phức tạp, không class, không Date.
 *
 * Vì vậy: giữ chỗ này càng ngu càng tốt. Mọi việc đóng/mở gói và kiểm tra kiểu
 * nằm ở `createNativeCaptureSource.ts`, nơi TypeScript thật sự bảo vệ được.
 *
 * Implementation:
 *   ios/CaptureSourceModule.swift      → CascableCore        (phase 1)
 *   android/CaptureSourceModule.kt     → libgphoto2 qua JNI  (phase sau)
 *
 * Cả hai phải ánh xạ lỗi riêng của SDK về `CaptureErrorCode` trong `types.ts`.
 * Nếu để lỗi thô của SDK lọt lên JS, UI sẽ phải parse chuỗi tiếng Anh và sẽ vỡ
 * khi thêm implementation thứ hai.
 */
export interface Spec extends TurboModule {
  startDiscovery(): void;
  stopDiscovery(): void;

  connect(cameraId: string): Promise<Object>;
  disconnect(cameraId: string): Promise<void>;

  /** `after` rỗng nghĩa là từ đầu. Trả mảng CaptureItemRef đã serialize. */
  listItems(cameraId: string, after: string, limit: number): Promise<Object[]>;

  fetchPreview(cameraId: string, itemId: string): Promise<Object>;
  fetchOriginal(cameraId: string, itemId: string): Promise<Object>;

  triggerShutter(cameraId: string): Promise<Object>;

  startLiveView(cameraId: string): Promise<void>;
  stopLiveView(cameraId: string): Promise<void>;

  readSettings(cameraId: string): Promise<Object>;
  writeSetting(cameraId: string, key: string, valueJson: string): Promise<void>;

  /** Bắt buộc cho NativeEventEmitter trên New Architecture. */
  addListener(eventName: string): void;
  removeListeners(count: number): void;
}

/**
 * `get` chứ không phải `getEnforcing`: Android phase 1 chưa có module này.
 * Tầng trên phải xử lý `null` bằng cách tắt tính năng tether, chứ không phải crash.
 */
export default TurboModuleRegistry.get<Spec>('CaptureSource');
