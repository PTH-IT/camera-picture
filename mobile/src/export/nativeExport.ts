import { NativeModules, Platform } from 'react-native';

/**
 * Cầu nối tới native module ghi file (ios/ImageExport).
 *
 * Trả `null` khi không có module thay vì ném lỗi: Android chưa có bản triển
 * khai, và bản xem trước trên trình duyệt cũng không có. Tầng trên kiểm null
 * rồi ẩn nút xuất — cùng cách xử lý với tether trên Android.
 */
interface ImageExportSpec {
  writeJPEG(base64: string, filename: string): Promise<string>;
  uploadFile(
    uri: string,
    url: string,
    method: string,
    headers: Record<string, string>,
  ): Promise<number>;
  remove(uri: string): Promise<void>;
}

const native = (NativeModules as Record<string, ImageExportSpec | undefined>).ImageExport ?? null;

export function canWriteFiles(): boolean {
  return native !== null;
}

/** Lý do không xuất được, để hiện cho người dùng thay vì một nút bấm không phản ứng. */
export function whyCannotExport(): string | null {
  if (native) return null;
  if (Platform.OS === 'android') return 'Xuất ảnh chưa có trên Android.';
  return 'Bản build này chưa có module xuất ảnh.';
}

export async function writeJPEG(base64: string, filename: string): Promise<string> {
  if (!native) throw new Error(whyCannotExport() ?? 'không xuất được');
  return native.writeJPEG(base64, filename);
}

/**
 * Tải file đã ghi lên kho lưu trữ, đọc thẳng từ đĩa ở phía native.
 *
 * Không dùng `fetch`: React Native không tạo được Blob từ ArrayBuffer, nên
 * không có cách nào gửi dữ liệu nhị phân thô từ JavaScript. Gửi base64 thì file
 * trên kho lưu trữ là văn bản, mở ra không phải ảnh — mà bước tải lên vẫn báo
 * thành công.
 */
export async function uploadFile(
  uri: string,
  url: string,
  method: string,
  headers: Record<string, string>,
): Promise<void> {
  if (!native) throw new Error(whyCannotExport() ?? 'không tải lên được');
  await native.uploadFile(uri, url, method, headers);
}

export async function removeExported(uri: string): Promise<void> {
  if (!native) return;
  try {
    await native.remove(uri);
  } catch {
    // Dọn file tạm hỏng không phải chuyện người dùng cần biết: ảnh đã chia sẻ
    // xong rồi, và lần xuất sau sẽ ghi đè lên chính file đó.
  }
}
