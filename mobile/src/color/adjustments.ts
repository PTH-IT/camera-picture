/**
 * Chỉnh màu thủ công — phần người dùng tự kéo, tách khỏi preset.
 *
 * Preset (LUT) là "look" có sẵn; những giá trị dưới đây là hiệu chỉnh cho TỪNG
 * tấm: bù sáng, cân bằng trắng, độ tương phản. Hai thứ đó khác nhau về bản chất
 * nên không gộp vào một khái niệm.
 *
 * MỌI giá trị nằm trong [-1, 1] và 0 là KHÔNG ĐỔI GÌ. Quy ước này quan trọng:
 * `overrides` rỗng và `overrides` toàn số 0 phải cho ra cùng một ảnh, nếu không
 * ảnh sẽ đổi màu chỉ vì người dùng chạm vào slider rồi kéo về chỗ cũ.
 *
 * Bản đối ứng phía máy chủ: `backend/internal/imaging/lut/adjust.go`. Hai bên
 * dùng CÙNG công thức và CÙNG thứ tự, và `TestAdjustmentsMatchMobile` đọc thẳng
 * file này cùng `haldLut.ts` để so từng hệ số — thêm hay bỏ một tham số mà quên
 * bên kia là test đỏ ngay, thay vì màu lệch âm thầm ở ảnh giao khách.
 */
export interface ColorAdjustments {
  /** Bù sáng, ±2 khẩu. */
  exposure: number;
  /** Tương phản quanh điểm giữa 0.5. */
  contrast: number;
  /** Độ bão hoà. -1 là đen trắng hoàn toàn. */
  saturation: number;
  /** Nhiệt độ: âm là lạnh (xanh), dương là ấm (vàng). */
  temperature: number;
  /** Sắc: âm là ngả lục, dương là ngả tím. */
  tint: number;
  /** Vùng sáng: kéo xuống để cứu trời cháy. */
  highlights: number;
  /** Vùng tối: kéo lên để mở bóng đổ. */
  shadows: number;
}

export const NEUTRAL_ADJUSTMENTS: ColorAdjustments = {
  exposure: 0,
  contrast: 0,
  saturation: 0,
  temperature: 0,
  tint: 0,
  highlights: 0,
  shadows: 0,
};

/** Thứ tự này PHẢI khớp thứ tự uniform trong shader. Xem haldLut.ts. */
export const ADJUSTMENT_KEYS = [
  'exposure',
  'contrast',
  'saturation',
  'temperature',
  'tint',
  'highlights',
  'shadows',
] as const;

export function isNeutral(adj: ColorAdjustments | null | undefined): boolean {
  if (!adj) return true;
  return ADJUSTMENT_KEYS.every(k => adj[k] === 0);
}

/**
 * Đọc chỉnh màu từ `overrides` của bản ghi chỉnh sửa.
 *
 * Dữ liệu này đi qua mạng và có thể do bản app cũ hơn ghi, nên mọi trường đều
 * phải kiểm kiểu và kẹp biên — một số ngoài [-1,1] lọt vào shader sẽ cho ra ảnh
 * cháy trắng mà không có lỗi nào.
 */
export function adjustmentsFrom(overrides: Record<string, unknown> | undefined): ColorAdjustments {
  const out = { ...NEUTRAL_ADJUSTMENTS };
  if (!overrides) return out;
  for (const key of ADJUSTMENT_KEYS) {
    const v = overrides[key];
    if (typeof v === 'number' && Number.isFinite(v)) {
      out[key] = Math.min(1, Math.max(-1, v));
    }
  }
  return out;
}

/** Chuyển thành `overrides` để gửi lên máy chủ. Bỏ hẳn phần trung tính. */
export function toOverrides(adj: ColorAdjustments): Record<string, number> {
  const out: Record<string, number> = {};
  for (const key of ADJUSTMENT_KEYS) {
    // Không gửi số 0: `overrides` trống và `overrides` toàn 0 là cùng một ảnh,
    // và bản ghi gọn hơn thì delta cũng nhẹ hơn.
    if (adj[key] !== 0) out[key] = adj[key];
  }
  return out;
}

/**
 * Xấp xỉ bằng CSS filter cho BẢN XEM TRƯỚC trên trình duyệt.
 *
 * KHÔNG khớp với shader — CSS không có vùng sáng/vùng tối riêng, và cân bằng
 * trắng chỉ xấp xỉ được bằng sepia + hue-rotate. Bản xem trước để kiểm bố cục
 * và luồng thao tác, không phải để đánh giá màu.
 */
export function toWebFilter(adj: ColorAdjustments): string {
  const parts: string[] = [];
  if (adj.exposure !== 0) parts.push(`brightness(${(1 + adj.exposure * 0.6).toFixed(3)})`);
  if (adj.contrast !== 0) parts.push(`contrast(${(1 + adj.contrast * 0.6).toFixed(3)})`);
  if (adj.saturation !== 0) parts.push(`saturate(${(1 + adj.saturation).toFixed(3)})`);
  if (adj.temperature !== 0) {
    parts.push(`sepia(${Math.abs(adj.temperature * 0.4).toFixed(3)})`);
    parts.push(`hue-rotate(${(adj.temperature < 0 ? 180 : 0).toFixed(0)}deg)`);
  }
  return parts.length > 0 ? parts.join(' ') : 'none';
}
