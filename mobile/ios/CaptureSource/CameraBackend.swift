import Foundation

/// Ranh giới giữa module React Native và SDK máy ảnh.
///
/// Vì sao có lớp trừu tượng này thay vì gọi thẳng CascableCore:
///
/// 1. Cho phép `MockBackend` chạy được ngay, nên toàn bộ luồng JavaScript — lưới
///    ảnh, áp màu, cull, đồng bộ — phát triển và kiểm thử được TRƯỚC khi có
///    license SDK, và cả trên máy không có máy ảnh cắm vào.
///
/// 2. CascableCore chỉ có trên nền tảng Apple. Bản Android (libgphoto2 qua JNI,
///    xem docs/adr/0002-auth-and-storage.md) sẽ cài đúng giao thức này, nên tầng
///    JavaScript không phải rẽ nhánh theo nền tảng.
///
/// 3. Nếu Cascable từ chối cấp license thương mại, phương án tự implement PTP/IP
///    qua WiFi (docs/adr/0003-capture-fallback.md) cũng chỉ là một implementation
///    khác của giao thức này.
protocol CameraBackend: AnyObject {
    var delegate: CameraBackendDelegate? { get set }

    func startDiscovery()
    func stopDiscovery()

    func connect(cameraID: String, completion: @escaping (Result<CameraInfo, Error>) -> Void)
    func disconnect(cameraID: String, completion: @escaping (Result<Void, Error>) -> Void)

    /// Duyệt thẻ nhớ. KHÔNG tải file.
    ///
    /// Phân trang vì một buổi chụp có thể vài nghìn tấm, và tải hết metadata một
    /// lần sẽ giữ giao diện đứng yên vài giây.
    func listItems(cameraID: String, after: String?, limit: Int,
                   completion: @escaping (Result<[CaptureItem], Error>) -> Void)

    /// Lấy JPEG preview nhúng.
    ///
    /// Nếu `capabilities` KHÔNG chứa `previewWithoutFullDownload`, bản triển khai
    /// buộc phải tải cả file RAW để lấy được preview — với NEF 55MB thì đó là
    /// thao tác hoàn toàn khác về chi phí, và tầng trên phải cảnh báo người dùng
    /// trước. Xem docs/adr/0001-capture-strategy.md.
    func fetchPreview(cameraID: String, itemID: String,
                      completion: @escaping (Result<LocalImage, Error>) -> Void)

    func fetchOriginal(cameraID: String, itemID: String,
                       progress: @escaping (Int64, Int64) -> Void,
                       completion: @escaping (Result<LocalImage, Error>) -> Void)

    func triggerShutter(cameraID: String, completion: @escaping (Result<CaptureItem, Error>) -> Void)

    func startLiveView(cameraID: String, completion: @escaping (Result<Void, Error>) -> Void)
    func stopLiveView(cameraID: String, completion: @escaping (Result<Void, Error>) -> Void)

    func readSettings(cameraID: String, completion: @escaping (Result<[String: Any], Error>) -> Void)
    func writeSetting(cameraID: String, key: String, valueJSON: String,
                      completion: @escaping (Result<Void, Error>) -> Void)
}

protocol CameraBackendDelegate: AnyObject {
    func backend(_ backend: CameraBackend, didDiscover camera: CameraInfo)
    func backend(_ backend: CameraBackend, didConnect camera: CameraInfo)
    func backend(_ backend: CameraBackend, didDisconnect cameraID: String, reason: String)

    /// Người dùng vừa bấm máy.
    ///
    /// `preview` có thể nil và tới sau: hiện chỗ trống ngay rồi điền ảnh khi có,
    /// đừng chặn giao diện chờ nó. Cả điểm hấp dẫn của tether nằm ở việc ảnh xuất
    /// hiện dưới một giây.
    func backend(_ backend: CameraBackend, didCapture item: CaptureItem, preview: LocalImage?)

    func backend(_ backend: CameraBackend, didReceiveLiveViewFrame frame: LocalImage)
    func backend(_ backend: CameraBackend, didFailWith code: CaptureErrorCode, message: String,
                 itemID: String?)
}

// MARK: - Kiểu dữ liệu

/// Khả năng của một đường capture cụ thể, với một body cụ thể, qua một transport
/// cụ thể.
///
/// Chuỗi ở đây PHẢI khớp `CameraCapability` trong mobile/src/capture/types.ts.
/// Không có codegen nối hai bên, nên sửa một bên thì sửa cả bên kia.
///
/// Không suy khả năng từ tên hãng. libgphoto2 không có live view với Nikon;
/// CascableCore thì có. Cùng một body, hai đường capture, hai tập khả năng.
enum CameraCapability: String {
    case remoteShutter
    case liveView
    case settingsRead
    case settingsWrite
    case tetheredEvents
    case storageBrowse
    case previewWithoutFullDownload
    case videoRecord
}

enum CaptureErrorCode: String {
    case permissionDenied
    case cameraBusy
    case connectionLost
    case unsupportedOperation
    case storageReadFailed
    case transferFailed
    case licenseInvalid
    case unknown
}

struct CameraInfo {
    let id: String
    let manufacturer: String
    let model: String
    let firmwareVersion: String?
    /// "usb" hoặc "wifi".
    let transport: String
    let capabilities: [CameraCapability]

    func toDictionary() -> [String: Any] {
        var d: [String: Any] = [
            "id": id,
            "manufacturer": manufacturer,
            "model": model,
            "transport": transport,
            "capabilities": capabilities.map { $0.rawValue },
        ]
        if let firmwareVersion { d["firmwareVersion"] = firmwareVersion }
        return d
    }
}

struct CaptureItem {
    let id: String
    let filename: String
    /// "NEF", "JPEG", ... — khớp `ImageFormat` bên TypeScript.
    let format: String
    let byteSize: Int64
    /// ISO 8601. Giờ của MÁY ẢNH, có thể lệch giờ điện thoại.
    let capturedAt: String
    let isRaw: Bool
    let hasEmbeddedPreview: Bool

    func toDictionary() -> [String: Any] {
        [
            "id": id,
            "filename": filename,
            "format": format,
            "byteSize": byteSize,
            "capturedAt": capturedAt,
            "isRaw": isRaw,
            "hasEmbeddedPreview": hasEmbeddedPreview,
        ]
    }
}

/// Tham chiếu tới pixel do phía native giữ.
///
/// `uri` trỏ tới file trong thư mục tạm của app. Bytes KHÔNG BAO GIỜ đi qua cầu
/// JavaScript: một NEF là 50-60MB, và đẩy nó qua cầu là giết hiệu năng và làm
/// OOM app. Đây là luật số 2 trong README.
struct LocalImage {
    let uri: String
    let width: Int
    let height: Int
    let byteSize: Int64

    func toDictionary() -> [String: Any] {
        ["uri": uri, "width": width, "height": height, "byteSize": byteSize]
    }
}

enum CaptureError: LocalizedError {
    case notConnected(String)
    case unsupported(String)
    case sdk(String)

    var errorDescription: String? {
        switch self {
        case .notConnected(let id): return "Chưa kết nối máy ảnh \(id)"
        case .unsupported(let op): return "Máy ảnh không hỗ trợ: \(op)"
        case .sdk(let msg): return msg
        }
    }

    var code: CaptureErrorCode {
        switch self {
        case .notConnected: return .connectionLost
        case .unsupported: return .unsupportedOperation
        case .sdk: return .unknown
        }
    }
}
