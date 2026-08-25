/**
 * Hệ thiết kế.
 *
 * Vì sao giao diện TỐI, và vì sao đây không phải lựa chọn thẩm mỹ:
 *
 * Mắt người đánh giá tông màu theo tương quan với vùng xung quanh. Một khung
 * giao diện sáng bao quanh tấm ảnh sẽ khiến chính tấm ảnh đó trông tối hơn và
 * bệt hơn thực tế; khung tối thì ngược lại. Với app mà giá trị cốt lõi là để
 * nhiếp ảnh gia và khách hàng phán xét MÀU, một giao diện sáng nghĩa là mọi
 * quyết định màu đều được đưa ra trên thông tin sai lệch.
 *
 * Đây là lý do Lightroom, Capture One, Photo Mechanic và mọi công cụ ảnh chuyên
 * nghiệp đều tối. Nếu có ai đề xuất thêm chế độ sáng, câu trả lời không phải
 * "không hợp gu" mà là "nó làm hỏng chính việc mà app này tồn tại để làm".
 *
 * Nền trung tính tuyệt đối (không ngả xanh, không ngả ấm) cũng vì lý do đó: nền
 * có sắc sẽ đẩy cảm nhận màu của ảnh theo hướng ngược lại.
 */

export const colors = {
  /** Nền sau ảnh khi xem toàn màn hình. Xám trung tính, không đen tuyệt đối —
   *  đen tuyệt đối làm vùng tối của ảnh biến mất vào nền và không đọc được. */
  canvas: '#141416',
  /** Nền chung của các màn hình danh sách. */
  background: '#0b0b0d',
  /** Bề mặt nổi: thẻ, thanh công cụ. */
  surface: '#1a1a1d',
  surfaceRaised: '#232327',
  border: '#2e2e33',

  text: '#f2f2f4',
  textMuted: '#a0a0a8',
  textFaint: '#6b6b73',

  /** Màu nhấn. Dùng dè dặt — mỗi vệt màu trong giao diện là một thứ cạnh tranh
   *  với màu của tấm ảnh. */
  accent: '#4c8dff',
  accentText: '#ffffff',

  success: '#3ecf8e',
  warning: '#f0b849',
  danger: '#ff5c5c',

  /** Trạng thái culling. */
  flagged: '#f0b849',
  rejected: '#ff5c5c',
} as const;

export const spacing = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 24,
  xxl: 32,
} as const;

export const radius = {
  sm: 6,
  md: 10,
  lg: 16,
  pill: 999,
} as const;

export const typography = {
  title: { fontSize: 22, fontWeight: '700' as const, color: colors.text },
  heading: { fontSize: 17, fontWeight: '600' as const, color: colors.text },
  body: { fontSize: 15, fontWeight: '400' as const, color: colors.text },
  label: { fontSize: 13, fontWeight: '600' as const, color: colors.textMuted },
  caption: { fontSize: 12, fontWeight: '400' as const, color: colors.textMuted },
  /** Số liệu kỹ thuật (ISO, khẩu, tốc). Chữ đều bề rộng để các con số không
   *  nhảy vị trí khi giá trị đổi — quan trọng vì chúng cập nhật liên tục. */
  mono: {
    fontSize: 12,
    fontWeight: '500' as const,
    color: colors.textMuted,
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
  },
} as const;

/** Kích thước tối thiểu cho vùng chạm. 44pt là hướng dẫn của Apple, và ở đây nó
 *  quan trọng hơn bình thường: người dùng thao tác khi đang cầm máy ảnh, đứng,
 *  và thường vội. */
export const HIT_SIZE = 44;
