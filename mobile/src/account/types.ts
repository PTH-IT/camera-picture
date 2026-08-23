/**
 * Hợp đồng xác thực và lựa chọn lưu trữ.
 *
 * Bản đối ứng phía server: backend/internal/auth và backend/internal/storage.
 * Quyết định kiến trúc và các ràng buộc bên ngoài: docs/adr/0002-auth-and-storage.md.
 */

export type AuthProvider = 'apple' | 'google' | 'password';

export interface User {
  readonly id: string;
  /**
   * Có thể RỖNG. Người dùng Sign in with Apple được phép ẩn email hoàn toàn,
   * và khi đó ta không có địa chỉ nào. Đừng viết UI giả định luôn có email.
   */
  readonly email?: string;
  readonly emailVerified: boolean;
  readonly name?: string;
}

export interface AuthResult {
  readonly token: string;
  readonly user: User;
}

/**
 * Yêu cầu đăng nhập bằng Apple hoặc Google.
 *
 * Hai điểm mà client BẮT BUỘC phải làm đúng, nếu không lớp bảo mật phía server
 * trở nên vô nghĩa:
 */
export interface OIDCSignInRequest {
  readonly provider: 'apple' | 'google';
  readonly idToken: string;

  /**
   * Nonce ngẫu nhiên, sinh MỚI cho mỗi lần đăng nhập.
   *
   * Client phải sinh nonce, băm SHA-256 rồi gửi bản băm cho Apple/Google, và gửi
   * bản GỐC lên server này. Server so khớp với claim `nonce` trong ID token.
   *
   * Không có nonce thì một ID token hợp lệ bị chặn được có thể phát lại để đăng
   * nhập dưới danh nghĩa nạn nhân. Server từ chối mọi yêu cầu thiếu nonce, nên
   * quên là đăng nhập hỏng ngay chứ không âm thầm mất an toàn.
   */
  readonly nonce: string;

  /**
   * Tên người dùng, CHỈ gửi ở lần uỷ quyền ĐẦU TIÊN với Apple.
   *
   * Apple trả tên cho client đúng một lần và không bao giờ trả lại — nó không
   * nằm trong ID token. Không chuyển tiếp ngay lần đầu là mất vĩnh viễn, không
   * có API nào lấy lại được. Đây là lỗi rất hay gặp khi tích hợp Sign in with Apple.
   */
  readonly name?: string;
}

/** Mã lỗi ổn định từ server. Xử lý theo mã, đừng parse chuỗi thông báo. */
export type AuthErrorCode =
  | 'unauthorized'
  | 'invalid_input'
  | 'conflict'
  /**
   * Email đã có tài khoản với phương thức đăng nhập khác.
   *
   * KHÔNG được tự động ghép — server từ chối là có chủ ý. UI phải yêu cầu người
   * dùng đăng nhập bằng phương thức cũ rồi liên kết tường minh. Xem
   * docs/adr/0002-auth-and-storage.md, mục quy tắc ghép tài khoản.
   */
  | 'link_required'
  | 'internal';

// ---------------------------------------------------------------------------
// Lưu trữ
// ---------------------------------------------------------------------------

export type StorageProvider = 'device' | 'managed' | 'google_drive' | 'icloud';

/**
 * Khả năng của một nhà cung cấp lưu trữ.
 *
 * Cùng khuôn mẫu với `CameraCapability` trong src/capture: rẽ nhánh theo KHẢ NĂNG
 * chứ không theo tên nhà cung cấp. `if (provider === 'icloud')` rải rác trong UI
 * là cách chắc chắn để bỏ sót một nhánh khi thêm nhà cung cấp thứ năm.
 */
export type StorageCapability =
  /**
   * Server đọc được bytes nên kết xuất RAW chất lượng cao phía máy chủ khả dụng.
   *
   * KHÔNG phải nhà cung cấp nào cũng có. Với `icloud` và `device`, server không
   * bao giờ thấy file — bản xuất chất lượng cao phải kết xuất trên thiết bị hoặc
   * không có. Đây là khác biệt tính năng thật, phải hiển thị TRƯỚC khi người dùng
   * chọn, không phải để họ phát hiện lúc bấm nút xuất file.
   */
  | 'serverSideRender'
  /** Hạn mức do app cưỡng chế. Chỉ đúng với `managed`. */
  | 'enforcedQuota'
  /** Dữ liệu không biến mất khi người dùng thu hồi quyền ở dịch vụ bên thứ ba. */
  | 'durable';

export interface StorageOption {
  readonly provider: StorageProvider;
  readonly capabilities: readonly StorageCapability[];
  /**
   * Cảnh báo mất dữ liệu, phải hiển thị NGAY tại màn hình chọn.
   *
   * Đây là yêu cầu sản phẩm, không phải trang trí. Với Drive và iCloud, dữ liệu
   * nằm ngoài tầm kiểm soát của app: người dùng hết dung lượng, thu hồi quyền,
   * hoặc xoá file trực tiếp thì ảnh biến mất và không khôi phục được. Giấu điều
   * này trong điều khoản sử dụng là khác biệt giữa một sản phẩm trung thực và
   * một vụ mất ảnh cưới.
   */
  readonly warning?: string;
}

export function hasCapability(o: StorageOption, c: StorageCapability): boolean {
  return o.capabilities.includes(c);
}

export interface StorageUsage {
  readonly provider: StorageProvider;
  readonly usedBytes: number;
  /**
   * 0 nghĩa là KHÔNG BIẾT hoặc không áp dụng — không phải "bằng không".
   * Với Drive và iCloud, ta chỉ biết nếu nhà cung cấp cho biết.
   */
  readonly limitBytes: number;
  readonly enforced: boolean;
}

/** Trả về số byte còn lại, hoặc `null` khi không xác định được. */
export function remainingBytes(u: StorageUsage): number | null {
  if (!u.enforced || u.limitBytes <= 0) return null;
  return Math.max(0, u.limitBytes - u.usedBytes);
}

// ---------------------------------------------------------------------------
// Mua dung lượng
// ---------------------------------------------------------------------------

export interface StorageProduct {
  readonly id: string;
  readonly name: string;
  readonly storageBytes: number;
}

/**
 * Đổi hoá đơn IAP lấy quyền lợi.
 *
 * Client CHỈ gửi hoá đơn thô. Không gửi kèm "gói này bao nhiêu GB" — server là
 * nơi duy nhất biết mỗi mã sản phẩm đáng bao nhiêu, và tự xác minh hoá đơn với
 * Apple/Google. Client tự khai đã mua là giả mạo được ngay.
 */
export interface RedeemPurchaseRequest {
  readonly platform: 'apple' | 'google';
  readonly receipt: string;
}

export interface Entitlement {
  readonly productId: string;
  readonly storageBytes: number;
  /** ISO 8601. */
  readonly expiresAt: string;
}

/**
 * Ghi chú thương mại quan trọng, không phải chi tiết kỹ thuật:
 *
 * Bán dung lượng do app quản lý là dịch vụ số, nên ở mọi storefront TRỪ Hoa Kỳ
 * thì bắt buộc qua In-App Purchase và Apple thu 15–30%. Con số đó phải nằm trong
 * mô hình giá ngay từ đầu.
 *
 * Liên kết Drive là lời giải CẤU TRÚC cho vấn đề này chứ không phải mẹo lách:
 * khi người dùng dùng Drive của chính họ, app không bán dung lượng nào cả — họ
 * mua từ Google, app chỉ bán chức năng. Vì vậy màn hình chọn nơi lưu trữ nên
 * trình bày Drive như một lựa chọn ngang hàng, không phải như phương án dự phòng.
 */
export const IAP_COMMISSION_NOTE = 'docs/adr/0002-auth-and-storage.md';
