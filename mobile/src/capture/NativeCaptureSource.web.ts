/**
 * Bản cho trình duyệt của lớp vận chuyển native.
 *
 * `react-native-web` KHÔNG export `TurboModuleRegistry`. Vì đó là named import
 * của ESM, lỗi xảy ra lúc LIÊN KẾT module — trước khi một dòng lệnh nào chạy —
 * nên không `try/catch` hay `?.` nào đỡ được: cả bản xem trước trắng màn hình,
 * mọi màn hình, chỉ vì một import mà không màn hình nào trong số đó dùng tới.
 *
 * Trả `null` không phải chắp vá cho bản xem trước mà là câu trả lời đúng:
 * trình duyệt không tether được. Tầng trên đã có sẵn đường xử lý cho trường hợp
 * đó — đúng cái đường mà Android phase 1 đi.
 *
 * Metro loại `.web.ts` khỏi `sourceExts` nên file này không bao giờ lọt vào bản
 * chạy trên máy thật; kiểu dữ liệu cũng vẫn lấy từ `NativeCaptureSource.ts` vì
 * tsconfig của mobile loại `*.web.ts` khỏi việc kiểm kiểu.
 */
export default null;
