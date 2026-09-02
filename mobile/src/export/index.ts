import { Share } from 'react-native';
import { renderGraded } from './render';
import { removeExported, uploadFile, whyCannotExport, writeJPEG } from './nativeExport';
import type { ApiClient } from '../api/client';
import type { ColorAdjustments } from '../color/adjustments';
import type { PresetVisual } from '../color/GradedImageProps';

export { canWriteFiles, whyCannotExport } from './nativeExport';
export type { RenderedImage } from './render';

export interface ExportRequest {
  /** Ảnh nguồn — JPEG preview nhúng, KHÔNG phải RAW. */
  uri: string;
  /** Tên file trên thẻ nhớ, dùng để đặt tên bản xuất. */
  filename: string;
  preset?: PresetVisual | null;
  amount: number;
  adjustments: ColorAdjustments;
}

/** Đổi đuôi sang .jpg. Bản xuất luôn là JPEG, kể cả khi bản gốc là NEF. */
function exportName(filename: string): string {
  const dot = filename.lastIndexOf('.');
  return (dot > 0 ? filename.slice(0, dot) : filename) + '.jpg';
}

/**
 * Xuất ra máy: kết xuất, ghi thành file, rồi mở bảng chia sẻ của hệ điều hành.
 *
 * Dùng bảng chia sẻ thay vì tự lưu vào thư viện ảnh: lưu vào thư viện cần quyền
 * truy cập toàn bộ ảnh của người dùng, và người dùng thường muốn gửi thẳng cho
 * khách qua Zalo/AirDrop chứ không phải để nó lẫn vào cuộn camera.
 */
export async function exportToDevice(req: ExportRequest): Promise<void> {
  const why = whyCannotExport();
  if (why) throw new Error(why);

  const rendered = await renderGraded(req.uri, req.preset, req.amount, req.adjustments);
  const fileUri = await writeJPEG(rendered.base64, exportName(req.filename));

  try {
    await Share.share({ url: fileUri, title: exportName(req.filename) });
  } finally {
    // Xoá sau khi bảng chia sẻ đóng. Giữ lại thì thư mục của app phình ra sau
    // mỗi buổi chụp mà người dùng không có cách nào dọn.
    await removeExported(fileUri);
  }
}

/**
 * Xuất lên kho lưu trữ (Drive, MinIO, …).
 *
 * Ba bước, và bước giữa KHÔNG đi qua API của mình: xin chỗ → tải thẳng lên nhà
 * cung cấp → báo đã xong. Cho ảnh chảy qua Go API sẽ giữ goroutine và băng
 * thông suốt thời gian tải.
 *
 * Khoá do máy chủ đặt và rơi vào thư mục `da-chinh/` của buổi chụp — client
 * không tự chọn đường dẫn.
 */
export async function exportToStorage(
  client: ApiClient,
  imageId: string,
  req: ExportRequest,
): Promise<string> {
  const why = whyCannotExport();
  if (why) throw new Error(why);

  const rendered = await renderGraded(req.uri, req.preset, req.amount, req.adjustments);

  // Ghi ra file trước rồi mới tải lên, thay vì giữ bytes trong JavaScript.
  // Native đọc thẳng từ đĩa, nên một file lớn cũng không bao giờ nằm trong bộ
  // nhớ của JS — đúng luật "pixel không đi qua cầu" của dự án.
  const fileUri = await writeJPEG(rendered.base64, exportName(req.filename));

  try {
    const target = await client.uploadTarget(imageId, 'export', rendered.byteSize);
    await uploadFile(fileUri, target.url, target.method, {
      ...(target.headers ?? {}),
      'Content-Type': 'image/jpeg',
    });

    await client.confirmAsset(imageId, {
      tier: 'export',
      storageKey: target.key,
      byteSize: rendered.byteSize,
      width: rendered.width,
      height: rendered.height,
    });
    return target.key;
  } finally {
    await removeExported(fileUri);
  }
}
