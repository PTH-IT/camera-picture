import Foundation

// import CascableCore
// import CascableCoreSwift
//
// Hai dòng trên bị chú thích cho tới khi SDK được thêm vào dự án. Xem
// mobile/ios/CaptureSource/README.md.

/// Bản triển khai `CameraBackend` dùng CascableCore.
///
/// TRẠNG THÁI: khung sườn. Phần bridging React Native và ranh giới `CameraBackend`
/// đã hoàn chỉnh; các chỗ gọi SDK được đánh dấu `NEEDS SDK` bên dưới.
///
/// Vì sao không viết sẵn lời gọi SDK: mình không có CascableCore để biên dịch và
/// kiểm chứng, và đoán tên hàm cho ra một file trông như đã xong nhưng sai —
/// tốn thời gian hơn nhiều so với một chỗ trống có chỉ dẫn chính xác. Mỗi mục
/// dưới đây ghi rõ cần tra cái gì trong `cascablecore-demo`.
///
/// Thứ tự làm trong 30 ngày dùng thử, theo mức độ rủi ro giảm dần:
///
///  1. `previewWithoutFullDownload` — giả định NGUY HIỂM NHẤT của kiến trúc.
///     Nếu CascableCore bắt buộc tải cả file RAW mới đọc được preview nhúng,
///     toàn bộ chiến lược "để ảnh trên thẻ" phải thiết kế lại. Kiểm tra TRƯỚC.
///  2. Độ phủ body — bảng compatibility công khai là của Cascable Studio, chưa
///     xác nhận cho SDK. Thử đúng danh sách Z6III/Z5II/Z50II/Zf/ZR.
///  3. Đo thời gian từ lúc bấm máy tới lúc preview hiện, qua USB-C và Wi-Fi.
///
/// Xem docs/adr/0001-capture-strategy.md.
final class CascableBackend: NSObject, CameraBackend {
    weak var delegate: CameraBackendDelegate?

    // private var discovery: CameraDiscovery?
    // private var cameras: [String: Camera] = [:]

    // MARK: - Tìm và kết nối

    func startDiscovery() {
        // NEEDS SDK — tra `CameraDiscovery` trong cascablecore-demo.
        //
        // Bật tìm cả USB lẫn network: người dùng có thể cắm cáp USB-C hoặc dùng
        // Wi-Fi của máy ảnh, và app không nên bắt họ chọn trước.
        //
        // Khi tìm thấy, ánh xạ sang CameraInfo và gọi
        // delegate?.backend(self, didDiscover: info)
        notImplemented("startDiscovery")
    }

    func stopDiscovery() {
        // NEEDS SDK. Nhớ dừng khi app vào background: quét liên tục ngốn pin,
        // và người dùng đang ở giữa một buổi chụp kéo dài nhiều giờ.
        notImplemented("stopDiscovery")
    }

    func connect(cameraID: String, completion: @escaping (Result<CameraInfo, Error>) -> Void) {
        // NEEDS SDK — tra `connect(authenticationRequestCallback:...)`.
        //
        // Một số body Nikon yêu cầu xác nhận trên thân máy khi kết nối lần đầu.
        // Callback xác thực PHẢI được nối lên giao diện để hiển thị hướng dẫn,
        // nếu không người dùng sẽ thấy app treo mà không hiểu vì sao.
        //
        // Sau khi kết nối, dựng capabilities bằng cách HỎI SDK về từng khả năng
        // — xem `mapCapabilities` bên dưới.
        completion(.failure(CaptureError.sdk("CascableCore chưa được tích hợp")))
    }

    func disconnect(cameraID: String, completion: @escaping (Result<Void, Error>) -> Void) {
        completion(.failure(CaptureError.sdk("CascableCore chưa được tích hợp")))
    }

    // MARK: - Duyệt thẻ và lấy ảnh

    func listItems(cameraID: String, after: String?, limit: Int,
                   completion: @escaping (Result<[CaptureItem], Error>) -> Void) {
        // NEEDS SDK — tra `camera.fileSystem` và việc duyệt storage device.
        //
        // QUAN TRỌNG: chỉ liệt kê metadata, KHÔNG tải file. Một buổi chụp là
        // hơn 100GB NEF và điện thoại không chứa nổi. Xem ADR 0001.
        completion(.failure(CaptureError.sdk("CascableCore chưa được tích hợp")))
    }

    func fetchPreview(cameraID: String, itemID: String,
                      completion: @escaping (Result<LocalImage, Error>) -> Void) {
        // NEEDS SDK — đây là chỗ QUAN TRỌNG NHẤT phải kiểm chứng trong bản dùng thử.
        //
        // Câu hỏi cần trả lời: CascableCore có lấy được JPEG preview nhúng mà
        // KHÔNG tải cả file RAW không?
        //
        // Nếu CÓ: đặt `.previewWithoutFullDownload` vào capabilities.
        // Nếu KHÔNG: BỎ nó ra, và tầng trên sẽ cảnh báo người dùng trước khi
        // tải — chứ đừng âm thầm tải 55MB cho mỗi lần chạm vào một ô trong lưới.
        //
        // Đừng "sửa" bằng cách vẫn khai capability rồi tải ngầm. Cả kiến trúc
        // lưu trữ dựa trên giả định này.
        completion(.failure(CaptureError.sdk("CascableCore chưa được tích hợp")))
    }

    func fetchOriginal(cameraID: String, itemID: String,
                       progress: @escaping (Int64, Int64) -> Void,
                       completion: @escaping (Result<LocalImage, Error>) -> Void) {
        // NEEDS SDK.
        //
        // Ghi thẳng ra file trong thư mục tạm theo từng khối, KHÔNG giữ cả file
        // trong bộ nhớ: một NEF là 55MB và người dùng có thể tải nhiều tấm cùng
        // lúc. Gọi `progress` đều đặn — qua Wi-Fi việc này mất hàng chục giây.
        completion(.failure(CaptureError.sdk("CascableCore chưa được tích hợp")))
    }

    // MARK: - Điều khiển

    func triggerShutter(cameraID: String, completion: @escaping (Result<CaptureItem, Error>) -> Void) {
        // NEEDS SDK — tra API chụp một phát trong demo.
        completion(.failure(CaptureError.sdk("CascableCore chưa được tích hợp")))
    }

    func startLiveView(cameraID: String, completion: @escaping (Result<Void, Error>) -> Void) {
        // NEEDS SDK — tra observer khung hình live view.
        //
        // Khung hình xử lý HOÀN TOÀN ở native rồi vẽ qua Skia. Đẩy từng khung
        // qua cầu JavaScript ở 30fps sẽ giết hiệu năng.
        //
        // Lưu ý về Nikon: libgphoto2 KHÔNG có live view với Nikon, nhưng
        // CascableCore thì có. Đừng suy giới hạn của thư viện này sang thư viện kia.
        completion(.failure(CaptureError.sdk("CascableCore chưa được tích hợp")))
    }

    func stopLiveView(cameraID: String, completion: @escaping (Result<Void, Error>) -> Void) {
        completion(.failure(CaptureError.sdk("CascableCore chưa được tích hợp")))
    }

    func readSettings(cameraID: String, completion: @escaping (Result<[String: Any], Error>) -> Void) {
        completion(.failure(CaptureError.sdk("CascableCore chưa được tích hợp")))
    }

    func writeSetting(cameraID: String, key: String, valueJSON: String,
                      completion: @escaping (Result<Void, Error>) -> Void) {
        completion(.failure(CaptureError.sdk("CascableCore chưa được tích hợp")))
    }

    // MARK: - Ánh xạ khả năng

    /// Dựng danh sách khả năng bằng cách HỎI SDK, không phải suy từ tên hãng.
    ///
    /// Đây là chỗ dễ đi tắt nhất và cũng là chỗ tốn kém nhất nếu đi tắt. Viết
    /// `if manufacturer == "Nikon" { ... }` sẽ sai ngay trong nội bộ dòng Z: các
    /// body khác nhau hỗ trợ khác nhau, và cùng một body qua USB với qua Wi-Fi
    /// cũng khác nhau.
    ///
    /// Toàn bộ giao diện rẽ nhánh theo danh sách này. Khai một khả năng mà máy
    /// không có nghĩa là hứa với người dùng thứ không tồn tại.
    private func mapCapabilities(/* camera: Camera */) -> [CameraCapability] {
        // NEEDS SDK — hỏi từng khả năng qua API của CascableCore.
        []
    }

    private func notImplemented(_ op: String) {
        delegate?.backend(self, didFailWith: .unsupportedOperation,
                          message: "CascableCore chưa được tích hợp: \(op)", itemID: nil)
    }
}
