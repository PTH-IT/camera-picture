import Foundation
import React

/// Ghi ảnh đã chỉnh ra file trong vùng của app.
///
/// Vì sao cần native module cho một việc nhỏ như thế này: React Native không có
/// API ghi file, và Skia chỉ trả về được base64 hoặc bytes trong bộ nhớ. Muốn
/// đưa ảnh cho người dùng — qua bảng chia sẻ của iOS, hoặc mở trong ứng dụng
/// khác — thì phải có một file thật với đường dẫn thật.
///
/// KHÔNG dùng react-native-fs: thư viện đó mang theo hàng chục API mà dự án
/// không cần, còn thứ cần ở đây là đúng một hàm.
///
/// Đối ứng phía JavaScript: mobile/src/export/nativeExport.ts
@objc(ImageExport)
final class ImageExportModule: NSObject {

    /// Không cần khởi tạo trên main queue: module này không đụng UIKit.
    @objc static func requiresMainQueueSetup() -> Bool { false }

    /// Thư mục chứa ảnh xuất.
    ///
    /// Đặt trong Documents chứ không phải thư mục tạm: thư mục tạm bị hệ điều
    /// hành xoá bất cứ lúc nào, kể cả trong lúc bảng chia sẻ đang mở, và khi đó
    /// người dùng bấm "Lưu ảnh" xong nhận về một lỗi không giải thích được.
    private static func exportDirectory() throws -> URL {
        let docs = try FileManager.default.url(
            for: .documentDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
        let dir = docs.appendingPathComponent("Xuat", isDirectory: true)
        if !FileManager.default.fileExists(atPath: dir.path) {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        }
        return dir
    }

    /// Làm sạch tên file.
    ///
    /// Tên đến từ tên file trên thẻ nhớ, tức là từ máy ảnh — không phải dữ liệu
    /// của ta. Một dấu gạch chéo trong đó sẽ ghi ra ngoài thư mục dự định.
    private static func safeName(_ raw: String) -> String {
        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "-_. "))
        let cleaned = String(raw.unicodeScalars.map { allowed.contains($0) ? Character($0) : "-" })
            .trimmingCharacters(in: CharacterSet(charactersIn: " .-"))
        return cleaned.isEmpty ? "anh-xuat.jpg" : cleaned
    }

    /// Ghi JPEG từ chuỗi base64 và trả về đường dẫn file.
    ///
    /// Nhận base64 chứ không phải bytes vì cầu JavaScript không truyền được dữ
    /// liệu nhị phân. Với ảnh xuất cỡ preview (~1–2MB) thì chi phí encode chấp
    /// nhận được; nếu sau này xuất bản full-res, phải đổi sang đường khác —
    /// base64 làm phình 33% và đi qua cầu JS hai lần.
    @objc(writeJPEG:filename:resolver:rejecter:)
    func writeJPEG(base64: String,
                   filename: String,
                   resolve: @escaping RCTPromiseResolveBlock,
                   reject: @escaping RCTPromiseRejectBlock) {
        // Bỏ tiền tố data URI nếu có: tuỳ phiên bản, Skia trả kèm hoặc không.
        var payload = base64
        if let comma = payload.range(of: ","), payload.hasPrefix("data:") {
            payload = String(payload[comma.upperBound...])
        }

        guard let data = Data(base64Encoded: payload, options: .ignoreUnknownCharacters) else {
            reject("decodeFailed", "Chuỗi base64 không hợp lệ", nil)
            return
        }

        do {
            let url = try Self.exportDirectory().appendingPathComponent(Self.safeName(filename))
            try data.write(to: url, options: .atomic)
            resolve(url.absoluteString)
        } catch {
            reject("writeFailed", "Không ghi được file: \(error.localizedDescription)", error)
        }
    }

    /// Tải một file đã ghi lên URL cho sẵn.
    ///
    /// Vì sao phải là native: React Native KHÔNG tạo được Blob từ ArrayBuffer
    /// ("Creating blobs from 'ArrayBuffer' and 'ArrayBufferView' are not
    /// supported"), nên không có cách nào gửi dữ liệu nhị phân thô bằng `fetch`.
    /// Gửi chuỗi base64 thì file trên kho lưu trữ là văn bản base64, mở ra không
    /// phải ảnh — và bước tải lên vẫn báo thành công.
    ///
    /// Làm ở đây còn đúng với luật của dự án: pixel không đi qua cầu JavaScript.
    /// `uploadTask(with:fromFile:)` đọc thẳng từ đĩa, nên một file 50MB cũng
    /// không bao giờ nằm trong bộ nhớ của JS.
    @objc(uploadFile:url:method:headers:resolver:rejecter:)
    func uploadFile(uri: String,
                    urlString: String,
                    method: String,
                    headers: [String: String],
                    resolve: @escaping RCTPromiseResolveBlock,
                    reject: @escaping RCTPromiseRejectBlock) {
        guard let fileURL = URL(string: uri), fileURL.isFileURL,
              FileManager.default.fileExists(atPath: fileURL.path) else {
            reject("fileMissing", "Không tìm thấy file cần tải lên", nil)
            return
        }
        guard let url = URL(string: urlString) else {
            reject("invalidURL", "URL tải lên không hợp lệ", nil)
            return
        }

        var req = URLRequest(url: url)
        req.httpMethod = method.isEmpty ? "PUT" : method
        for (k, v) in headers {
            req.setValue(v, forHTTPHeaderField: k)
        }

        let task = URLSession.shared.uploadTask(with: req, fromFile: fileURL) { _, response, error in
            if let error {
                reject("uploadFailed", error.localizedDescription, error)
                return
            }
            let status = (response as? HTTPURLResponse)?.statusCode ?? 0
            if !(200...299).contains(status) {
                // Trả mã trạng thái ra ngoài: lỗi ở đây là của NHÀ CUNG CẤP, và
                // người đọc log cần biết đi hỏi ai.
                reject("uploadRejected", "Nơi lưu trữ từ chối (HTTP \(status))", nil)
                return
            }
            resolve(status)
        }
        task.resume()
    }

    /// Xoá file đã xuất.
    ///
    /// Ảnh xuất là bản tạm để chia sẻ; giữ lại thì thư mục của app phình ra sau
    /// mỗi buổi chụp và người dùng không có cách nào dọn.
    @objc(remove:resolver:rejecter:)
    func remove(uri: String,
                resolve: @escaping RCTPromiseResolveBlock,
                reject: @escaping RCTPromiseRejectBlock) {
        guard let url = URL(string: uri), url.isFileURL else {
            reject("invalidURI", "Đường dẫn không hợp lệ", nil)
            return
        }
        do {
            if FileManager.default.fileExists(atPath: url.path) {
                try FileManager.default.removeItem(at: url)
            }
            resolve(nil)
        } catch {
            reject("removeFailed", error.localizedDescription, error)
        }
    }
}
