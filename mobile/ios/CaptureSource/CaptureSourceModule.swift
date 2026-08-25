import Foundation
import React

/// Module React Native cho tầng capture.
///
/// Phần bridging ở đây đã hoàn chỉnh và không phụ thuộc SDK nào. Việc gọi SDK
/// máy ảnh nằm sau `CameraBackend`, nên module này chạy được ngay với
/// `MockBackend` trong lúc chờ license CascableCore.
///
/// Đối ứng phía JavaScript: mobile/src/capture/NativeCaptureSource.ts
@objc(CaptureSource)
final class CaptureSourceModule: RCTEventEmitter {

    private var backend: CameraBackend = MockBackend()
    private var hasListeners = false

    /// Hàng đợi riêng cho mọi thao tác với máy ảnh.
    ///
    /// SDK máy ảnh thường KHÔNG an toàn khi gọi đồng thời trên cùng một handle,
    /// và JavaScript có thể bắn nhiều lệnh liên tiếp (người dùng lướt nhanh qua
    /// lưới ảnh sẽ kích hoạt hàng loạt fetchPreview). Tuần tự hoá ở một chỗ rẻ
    /// hơn nhiều so với truy một lỗi hỏng bộ nhớ chỉ xảy ra khi thao tác nhanh.
    private let queue = DispatchQueue(label: "vn.pth.camera.capture", qos: .userInitiated)

    override init() {
        super.init()
        backend.delegate = self
    }

    /// Cho phép thay backend lúc chạy (Cascable, mock, hoặc bản tự implement
    /// PTP/IP nếu không dùng được Cascable — xem ADR 0003).
    func setBackend(_ newBackend: CameraBackend) {
        queue.async {
            self.backend.stopDiscovery()
            self.backend.delegate = nil
            self.backend = newBackend
            newBackend.delegate = self
        }
    }

    // MARK: - RCTEventEmitter

    override static func requiresMainQueueSetup() -> Bool { false }

    override func supportedEvents() -> [String] {
        ["captureEvent"]
    }

    override func startObserving() { hasListeners = true }
    override func stopObserving() { hasListeners = false }

    /// Gửi sự kiện lên JavaScript.
    ///
    /// Bỏ qua khi chưa có listener: gửi sự kiện lúc không ai nghe sẽ khiến React
    /// Native cảnh báo và, ở bản build cũ, làm rò bộ nhớ.
    private func emit(_ payload: [String: Any]) {
        guard hasListeners else { return }
        sendEvent(withName: "captureEvent", body: payload)
    }

    // MARK: - Phương thức gọi từ JavaScript

    @objc func startDiscovery() {
        queue.async { self.backend.startDiscovery() }
    }

    @objc func stopDiscovery() {
        queue.async { self.backend.stopDiscovery() }
    }

    @objc(connect:resolver:rejecter:)
    func connect(cameraID: String, resolve: @escaping RCTPromiseResolveBlock,
                 reject: @escaping RCTPromiseRejectBlock) {
        queue.async {
            self.backend.connect(cameraID: cameraID) { result in
                self.settle(result.map { $0.toDictionary() }, resolve, reject)
            }
        }
    }

    @objc(disconnect:resolver:rejecter:)
    func disconnect(cameraID: String, resolve: @escaping RCTPromiseResolveBlock,
                    reject: @escaping RCTPromiseRejectBlock) {
        queue.async {
            self.backend.disconnect(cameraID: cameraID) { result in
                self.settle(result.map { _ in NSNull() }, resolve, reject)
            }
        }
    }

    @objc(listItems:after:limit:resolver:rejecter:)
    func listItems(cameraID: String, after: String, limit: NSNumber,
                   resolve: @escaping RCTPromiseResolveBlock,
                   reject: @escaping RCTPromiseRejectBlock) {
        queue.async {
            // Chuỗi rỗng nghĩa là "từ đầu". Dùng chuỗi thay vì null vì codegen
            // của React Native không nhận kiểu nullable cho tham số chuỗi.
            let cursor = after.isEmpty ? nil : after
            self.backend.listItems(cameraID: cameraID, after: cursor, limit: limit.intValue) { result in
                self.settle(result.map { $0.map { $0.toDictionary() } }, resolve, reject)
            }
        }
    }

    @objc(fetchPreview:itemID:resolver:rejecter:)
    func fetchPreview(cameraID: String, itemID: String,
                      resolve: @escaping RCTPromiseResolveBlock,
                      reject: @escaping RCTPromiseRejectBlock) {
        queue.async {
            self.backend.fetchPreview(cameraID: cameraID, itemID: itemID) { result in
                self.settle(result.map { $0.toDictionary() }, resolve, reject)
            }
        }
    }

    @objc(fetchOriginal:itemID:resolver:rejecter:)
    func fetchOriginal(cameraID: String, itemID: String,
                       resolve: @escaping RCTPromiseResolveBlock,
                       reject: @escaping RCTPromiseRejectBlock) {
        queue.async {
            self.backend.fetchOriginal(cameraID: cameraID, itemID: itemID, progress: { done, total in
                // Tiến độ đi bằng SỰ KIỆN chứ không phải promise: một NEF 60MB
                // qua Wi-Fi mất hàng chục giây, và người dùng phải thấy thanh
                // tiến độ nhúc nhích thay vì một màn hình đứng yên.
                self.emit([
                    "type": "transferProgress",
                    "itemId": itemID,
                    "bytesTransferred": done,
                    "bytesTotal": total,
                ])
            }, completion: { result in
                self.settle(result.map { $0.toDictionary() }, resolve, reject)
            })
        }
    }

    @objc(triggerShutter:resolver:rejecter:)
    func triggerShutter(cameraID: String, resolve: @escaping RCTPromiseResolveBlock,
                        reject: @escaping RCTPromiseRejectBlock) {
        queue.async {
            self.backend.triggerShutter(cameraID: cameraID) { result in
                self.settle(result.map { $0.toDictionary() }, resolve, reject)
            }
        }
    }

    @objc(startLiveView:resolver:rejecter:)
    func startLiveView(cameraID: String, resolve: @escaping RCTPromiseResolveBlock,
                       reject: @escaping RCTPromiseRejectBlock) {
        queue.async {
            self.backend.startLiveView(cameraID: cameraID) { result in
                self.settle(result.map { _ in NSNull() }, resolve, reject)
            }
        }
    }

    @objc(stopLiveView:resolver:rejecter:)
    func stopLiveView(cameraID: String, resolve: @escaping RCTPromiseResolveBlock,
                      reject: @escaping RCTPromiseRejectBlock) {
        queue.async {
            self.backend.stopLiveView(cameraID: cameraID) { result in
                self.settle(result.map { _ in NSNull() }, resolve, reject)
            }
        }
    }

    @objc(readSettings:resolver:rejecter:)
    func readSettings(cameraID: String, resolve: @escaping RCTPromiseResolveBlock,
                      reject: @escaping RCTPromiseRejectBlock) {
        queue.async {
            self.backend.readSettings(cameraID: cameraID) { result in
                self.settle(result.map { $0 as Any }, resolve, reject)
            }
        }
    }

    @objc(writeSetting:key:valueJSON:resolver:rejecter:)
    func writeSetting(cameraID: String, key: String, valueJSON: String,
                      resolve: @escaping RCTPromiseResolveBlock,
                      reject: @escaping RCTPromiseRejectBlock) {
        queue.async {
            self.backend.writeSetting(cameraID: cameraID, key: key, valueJSON: valueJSON) { result in
                self.settle(result.map { _ in NSNull() }, resolve, reject)
            }
        }
    }

    // MARK: - Trợ giúp

    /// Trả promise về JavaScript trên main queue.
    ///
    /// Ánh xạ lỗi về `CaptureErrorCode` chứ không trả nguyên lỗi của SDK: nếu để
    /// lỗi thô lọt lên, giao diện sẽ phải so khớp chuỗi tiếng Anh của Cascable —
    /// và sẽ vỡ khi thêm bản Android dùng libgphoto2 với thông báo hoàn toàn khác.
    private func settle<T>(_ result: Result<T, Error>,
                           _ resolve: @escaping RCTPromiseResolveBlock,
                           _ reject: @escaping RCTPromiseRejectBlock) {
        DispatchQueue.main.async {
            switch result {
            case .success(let value):
                resolve(value)
            case .failure(let error):
                let code = (error as? CaptureError)?.code ?? .unknown
                reject(code.rawValue, error.localizedDescription, error)
            }
        }
    }
}

// MARK: - CameraBackendDelegate

extension CaptureSourceModule: CameraBackendDelegate {
    func backend(_ backend: CameraBackend, didDiscover camera: CameraInfo) {
        emit(["type": "cameraDiscovered", "camera": camera.toDictionary()])
    }

    func backend(_ backend: CameraBackend, didConnect camera: CameraInfo) {
        emit(["type": "cameraConnected", "camera": camera.toDictionary()])
    }

    func backend(_ backend: CameraBackend, didDisconnect cameraID: String, reason: String) {
        emit(["type": "cameraDisconnected", "cameraId": cameraID, "reason": reason])
    }

    func backend(_ backend: CameraBackend, didCapture item: CaptureItem, preview: LocalImage?) {
        var payload: [String: Any] = ["type": "itemCaptured", "item": item.toDictionary()]
        if let preview { payload["preview"] = preview.toDictionary() }
        emit(payload)
    }

    func backend(_ backend: CameraBackend, didReceiveLiveViewFrame frame: LocalImage) {
        emit(["type": "liveViewFrame", "handle": frame.toDictionary()])
    }

    func backend(_ backend: CameraBackend, didFailWith code: CaptureErrorCode, message: String,
                 itemID: String?) {
        var payload: [String: Any] = ["type": "error", "code": code.rawValue, "message": message]
        if let itemID { payload["itemId"] = itemID }
        emit(payload)
    }
}
