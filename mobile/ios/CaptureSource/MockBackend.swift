import Foundation
import UIKit

/// Máy ảnh giả, chạy được thật.
///
/// Không phải đồ chơi — đây là thứ cho phép phát triển và kiểm thử TOÀN BỘ luồng
/// ứng dụng trước khi có license CascableCore, và sau đó vẫn dùng để:
///
///  - Chạy trên simulator, nơi không cắm được máy ảnh
///  - Kiểm thử tự động trên CI mà không cần phần cứng
///  - Tái hiện các trường hợp khó dựng bằng máy thật: thẻ đầy, rớt kết nối giữa
///    lúc truyền, ảnh không có preview nhúng
///
/// Điểm cuối là lý do quan trọng nhất. Những lỗi tệ nhất của tether xảy ra đúng
/// vào lúc mọi thứ không suôn sẻ, và dựng lại chúng bằng máy ảnh thật vừa chậm
/// vừa không lặp lại được.
final class MockBackend: CameraBackend {
    weak var delegate: CameraBackendDelegate?

    /// Khả năng mô phỏng. Đặt `previewWithoutFullDownload = false` để kiểm thử
    /// nhánh mà tầng trên phải cảnh báo người dùng trước khi tải cả file RAW.
    var capabilities: [CameraCapability] = [
        .remoteShutter, .liveView, .settingsRead, .settingsWrite,
        .tetheredEvents, .storageBrowse, .previewWithoutFullDownload,
    ]

    /// Bật để mô phỏng rớt kết nối sau `dropAfterSeconds`.
    var simulateDisconnect = false
    var dropAfterSeconds: TimeInterval = 20

    private let cameraID = "mock-nikon-z8"
    private var connected = false
    private var shotCount = 0
    private var captureTimer: Timer?
    private let queue = DispatchQueue(label: "vn.pth.camera.mock")

    private var info: CameraInfo {
        CameraInfo(
            id: cameraID,
            manufacturer: "Nikon",
            model: "Z 8 (giả lập)",
            firmwareVersion: "2.10",
            transport: "wifi",
            capabilities: capabilities
        )
    }

    func startDiscovery() {
        // Trễ một chút cho giống thật: máy ảnh không xuất hiện tức thì, và giao
        // diện phải xử lý được khoảng chờ đó thay vì giả định có ngay.
        queue.asyncAfter(deadline: .now() + 0.6) { [weak self] in
            guard let self else { return }
            self.delegate?.backend(self, didDiscover: self.info)
        }
    }

    func stopDiscovery() {}

    func connect(cameraID: String, completion: @escaping (Result<CameraInfo, Error>) -> Void) {
        queue.asyncAfter(deadline: .now() + 0.4) { [weak self] in
            guard let self else { return }
            self.connected = true
            completion(.success(self.info))
            self.delegate?.backend(self, didConnect: self.info)
            self.startFakeShooting()

            if self.simulateDisconnect {
                self.queue.asyncAfter(deadline: .now() + self.dropAfterSeconds) {
                    self.connected = false
                    self.captureTimer?.invalidate()
                    self.delegate?.backend(self, didDisconnect: self.cameraID,
                                           reason: "Mất kết nối Wi-Fi (giả lập)")
                }
            }
        }
    }

    func disconnect(cameraID: String, completion: @escaping (Result<Void, Error>) -> Void) {
        queue.async { [weak self] in
            self?.connected = false
            self?.captureTimer?.invalidate()
            completion(.success(()))
        }
    }

    func listItems(cameraID: String, after: String?, limit: Int,
                   completion: @escaping (Result<[CaptureItem], Error>) -> Void) {
        guard connected else { return completion(.failure(CaptureError.notConnected(cameraID))) }
        queue.asyncAfter(deadline: .now() + 0.2) { [weak self] in
            guard let self else { return }
            let start = after.flatMap { Int($0.split(separator: "-").last ?? "") }.map { $0 + 1 } ?? 0
            let items = (start..<min(start + limit, self.shotCount)).map { self.item(index: $0) }
            completion(.success(items))
        }
    }

    func fetchPreview(cameraID: String, itemID: String,
                      completion: @escaping (Result<LocalImage, Error>) -> Void) {
        guard connected else { return completion(.failure(CaptureError.notConnected(cameraID))) }
        // Preview nhúng nhanh vì nó đã nằm sẵn trong file, không phải decode RAW.
        queue.asyncAfter(deadline: .now() + 0.15) { [weak self] in
            guard let self else { return }
            completion(self.makeImage(seed: itemID, width: 1620, height: 1080))
        }
    }

    func fetchOriginal(cameraID: String, itemID: String,
                       progress: @escaping (Int64, Int64) -> Void,
                       completion: @escaping (Result<LocalImage, Error>) -> Void) {
        guard connected else { return completion(.failure(CaptureError.notConnected(cameraID))) }
        let total: Int64 = 55 * 1024 * 1024
        // Mô phỏng tốc độ Wi-Fi thật: ~55MB mất khoảng 10 giây. Giao diện phải
        // dùng được trong suốt khoảng đó, không phải đứng yên.
        var done: Int64 = 0
        queue.async {
            for _ in 0..<20 {
                Thread.sleep(forTimeInterval: 0.5)
                done += total / 20
                progress(min(done, total), total)
            }
            completion(self.makeImage(seed: itemID + "-full", width: 8256, height: 5504))
        }
    }

    func triggerShutter(cameraID: String, completion: @escaping (Result<CaptureItem, Error>) -> Void) {
        guard connected else { return completion(.failure(CaptureError.notConnected(cameraID))) }
        guard capabilities.contains(.remoteShutter) else {
            return completion(.failure(CaptureError.unsupported("bấm chụp từ xa")))
        }
        queue.async { [weak self] in
            guard let self else { return }
            let item = self.emitCapture()
            completion(.success(item))
        }
    }

    func startLiveView(cameraID: String, completion: @escaping (Result<Void, Error>) -> Void) {
        guard capabilities.contains(.liveView) else {
            return completion(.failure(CaptureError.unsupported("live view")))
        }
        completion(.success(()))
    }

    func stopLiveView(cameraID: String, completion: @escaping (Result<Void, Error>) -> Void) {
        completion(.success(()))
    }

    func readSettings(cameraID: String, completion: @escaping (Result<[String: Any], Error>) -> Void) {
        completion(.success(["iso": 400, "aperture": "f/1.8", "shutter": "1/200", "focal": "85mm"]))
    }

    func writeSetting(cameraID: String, key: String, valueJSON: String,
                      completion: @escaping (Result<Void, Error>) -> Void) {
        guard capabilities.contains(.settingsWrite) else {
            return completion(.failure(CaptureError.unsupported("ghi thông số")))
        }
        completion(.success(()))
    }

    // MARK: - Nội bộ

    /// Bắn ảnh về đều đặn, như một buổi chụp thật đang diễn ra.
    private func startFakeShooting() {
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.captureTimer?.invalidate()
            self.captureTimer = Timer.scheduledTimer(withTimeInterval: 3.5, repeats: true) { [weak self] _ in
                self?.queue.async { _ = self?.emitCapture() }
            }
        }
    }

    @discardableResult
    private func emitCapture() -> CaptureItem {
        let item = self.item(index: shotCount)
        shotCount += 1

        // Sự kiện đi TRƯỚC, preview theo sau. Giao diện phải hiện chỗ trống ngay
        // rồi điền ảnh khi có — cả điểm hấp dẫn của tether nằm ở việc ảnh xuất
        // hiện dưới một giây.
        delegate?.backend(self, didCapture: item, preview: nil)

        queue.asyncAfter(deadline: .now() + 0.35) { [weak self] in
            guard let self, case .success(let img) = self.makeImage(seed: item.id, width: 1620, height: 1080) else { return }
            self.delegate?.backend(self, didCapture: item, preview: img)
        }
        return item
    }

    private func item(index: Int) -> CaptureItem {
        let fmt = ISO8601DateFormatter()
        return CaptureItem(
            id: "mock-item-\(index)",
            filename: String(format: "DSC_%04d.NEF", 4000 + index),
            format: "NEF",
            byteSize: 55 * 1024 * 1024,
            capturedAt: fmt.string(from: Date()),
            isRaw: true,
            hasEmbeddedPreview: capabilities.contains(.previewWithoutFullDownload)
        )
    }

    /// Sinh ảnh giả và ghi ra thư mục tạm.
    ///
    /// Ghi ra FILE chứ không trả bytes: hợp đồng nói pixel không bao giờ đi qua
    /// cầu JavaScript, và bản giả phải tuân thủ đúng hợp đồng đó — nếu không nó
    /// sẽ giấu đi chính vấn đề hiệu năng mà hợp đồng tồn tại để tránh.
    private func makeImage(seed: String, width: Int, height: Int) -> Result<LocalImage, Error> {
        let size = CGSize(width: width / 4, height: height / 4)
        let renderer = UIGraphicsImageRenderer(size: size)
        let hue = CGFloat(abs(seed.hashValue % 100)) / 100.0

        let image = renderer.image { ctx in
            UIColor(hue: hue, saturation: 0.25, brightness: 0.85, alpha: 1).setFill()
            ctx.fill(CGRect(origin: .zero, size: size))
            UIColor(hue: hue, saturation: 0.35, brightness: 0.55, alpha: 0.6).setFill()
            ctx.cgContext.fillEllipse(in: CGRect(x: size.width * 0.25, y: size.height * 0.3,
                                                 width: size.width * 0.5, height: size.height * 0.6))
        }

        guard let data = image.jpegData(compressionQuality: 0.85) else {
            return .failure(CaptureError.sdk("không tạo được ảnh giả"))
        }
        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent("mock-\(abs(seed.hashValue)).jpg")
        do {
            try data.write(to: url)
        } catch {
            return .failure(error)
        }
        return .success(LocalImage(uri: url.absoluteString,
                                   width: Int(size.width), height: Int(size.height),
                                   byteSize: Int64(data.count)))
    }
}
