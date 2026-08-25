/**
 * Dữ liệu mẫu cho bản xem trước giao diện.
 *
 * CHỈ dùng cho preview và Storybook-style development. Không được import từ mã
 * sản phẩm — nếu một màn hình cần dữ liệu mẫu để chạy, đó là dấu hiệu nó đang
 * thiếu trạng thái rỗng tử tế.
 */

import type { StorageOption, StorageUsage } from '../account/types';

export interface DemoShot {
  id: string;
  filename: string;
  uri: string;
  /** Giờ máy ảnh, dạng hiển thị. */
  time: string;
  iso: number;
  aperture: string;
  shutter: string;
  focal: string;
  rating: number;
  flagged: boolean;
  rejected: boolean;
  /** Bản gốc đã lên server chưa. false là trạng thái BÌNH THƯỜNG — phần lớn ảnh
   *  nằm trên thẻ suốt buổi chụp. */
  originalUploaded: boolean;
}

const SHUTTERS = ['1/200', '1/250', '1/400', '1/160', '1/320', '1/500'];
const APERTURES = ['f/1.8', 'f/2.0', 'f/2.8', 'f/1.4', 'f/4.0'];
const FOCALS = ['85mm', '50mm', '35mm', '135mm', '24mm'];

export function demoShots(count = 12): DemoShot[] {
  return Array.from({ length: count }, (_, i) => {
    const n = 4821 + i;
    return {
      id: `img-${i}`,
      filename: `DSC_${n}.NEF`,
      uri: `/shot-${String(i % 12).padStart(2, '0')}.jpg`,
      time: `16:${String(12 + i * 2).padStart(2, '0')}:${String((i * 17) % 60).padStart(2, '0')}`,
      iso: [100, 200, 400, 640, 1250][i % 5]!,
      aperture: APERTURES[i % APERTURES.length]!,
      shutter: SHUTTERS[i % SHUTTERS.length]!,
      focal: FOCALS[i % FOCALS.length]!,
      rating: i === 0 ? 5 : i === 3 ? 4 : 0,
      flagged: i === 0 || i === 3,
      rejected: i === 7,
      originalUploaded: i < 2,
    };
  });
}

export interface DemoPreset {
  id: string;
  name: string;
  /** Xấp xỉ bằng CSS filter, CHỈ để bản xem trước trên web có gì đó nhìn được.
   *  Trên máy thật, màu do LUT hald 3D chạy trong shader Skia quyết định — xem
   *  docs/hald-lut-format.md. Hai thứ này KHÔNG khớp nhau và không nhằm khớp. */
  webFilter: string;
}

export const demoPresets: DemoPreset[] = [
  { id: 'none', name: 'Gốc', webFilter: 'none' },
  { id: 'warm', name: 'Wedding Warm', webFilter: 'saturate(1.12) sepia(0.18) contrast(1.05)' },
  { id: 'film', name: 'Film 400', webFilter: 'saturate(0.88) contrast(1.12) brightness(1.04)' },
  { id: 'airy', name: 'Airy', webFilter: 'brightness(1.12) saturate(0.94) contrast(0.94)' },
  { id: 'moody', name: 'Moody', webFilter: 'brightness(0.92) saturate(0.9) contrast(1.18)' },
  { id: 'red', name: 'RED Cine', webFilter: 'saturate(1.05) contrast(1.14) hue-rotate(-6deg)' },
];

export const demoCamera = {
  model: 'Nikon Z8',
  transport: 'wifi' as const,
  battery: 78,
  /** Khả năng do chính đường capture báo về, không suy ra từ tên hãng.
   *  Xem chú thích trong src/capture/types.ts. */
  capabilities: ['tetheredEvents', 'storageBrowse', 'previewWithoutFullDownload', 'liveView'],
};

export const demoSessions = [
  { id: 's1', name: 'Minh & Lan — Tiệc cưới', client: 'Minh', date: 'Hôm nay', shots: 247, live: true },
  { id: 's2', name: 'Ảnh cưới ngoại cảnh — Đà Lạt', client: 'Huy & Trang', date: '22/08', shots: 1284, live: false },
  { id: 's3', name: 'Studio — Áo dài', client: 'Ngọc', date: '19/08', shots: 386, live: false },
  { id: 's4', name: 'Kỷ yếu THPT Lê Quý Đôn', client: 'Lớp 12A3', date: '15/08', shots: 902, live: false },
];

export const demoStorageOptions: StorageOption[] = [
  {
    provider: 'device',
    capabilities: [],
    warning: 'Ảnh chỉ nằm trên thẻ nhớ và điện thoại. Mất máy hoặc mất thẻ là mất ảnh.',
  },
  {
    provider: 'managed',
    capabilities: ['serverSideRender', 'enforcedQuota', 'durable'],
  },
  {
    provider: 'google_drive',
    capabilities: ['serverSideRender'],
    warning:
      'Ảnh lưu trong Google Drive của bạn. Nếu bạn hết dung lượng Drive, thu hồi quyền truy cập, ' +
      'hoặc xoá file trực tiếp trong Drive, ảnh sẽ biến mất khỏi ứng dụng và không khôi phục được.',
  },
];

export const demoUsage: StorageUsage = {
  provider: 'managed',
  usedBytes: 1.4 * 1024 ** 3,
  limitBytes: 2 * 1024 ** 3,
  enforced: true,
};

export function formatBytes(n: number): string {
  if (n >= 1024 ** 4) return `${(n / 1024 ** 4).toFixed(1)} TB`;
  if (n >= 1024 ** 3) return `${(n / 1024 ** 3).toFixed(1)} GB`;
  if (n >= 1024 ** 2) return `${Math.round(n / 1024 ** 2)} MB`;
  return `${Math.round(n / 1024)} KB`;
}
