#import <React/RCTBridgeModule.h>

// Khai báo cầu nối cho lớp Swift. Thiếu một dòng ở đây thì phương thức tương
// ứng không tồn tại phía JavaScript, và lỗi chỉ lộ ra lúc chạy dưới dạng
// "undefined is not a function".
@interface RCT_EXTERN_MODULE (ImageExport, NSObject)

RCT_EXTERN_METHOD(writeJPEG:(NSString *)base64
                  filename:(NSString *)filename
                  resolver:(RCTPromiseResolveBlock)resolve
                  rejecter:(RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(uploadFile:(NSString *)uri
                  url:(NSString *)url
                  method:(NSString *)method
                  headers:(NSDictionary *)headers
                  resolver:(RCTPromiseResolveBlock)resolve
                  rejecter:(RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(remove:(NSString *)uri
                  resolver:(RCTPromiseResolveBlock)resolve
                  rejecter:(RCTPromiseRejectBlock)reject)

@end
