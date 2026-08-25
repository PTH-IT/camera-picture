#import <React/RCTBridgeModule.h>
#import <React/RCTEventEmitter.h>

// Khai báo cầu nối cho lớp Swift.
//
// Swift không tự đăng ký được với React Native; phải có file Objective-C này
// khai báo từng phương thức. Thiếu một dòng ở đây thì phương thức tương ứng
// không tồn tại phía JavaScript, và lỗi chỉ lộ ra lúc chạy dưới dạng
// "undefined is not a function" — không phải lỗi biên dịch.
//
// Tên và thứ tự tham số PHẢI khớp thuộc tính @objc trong CaptureSourceModule.swift.

@interface RCT_EXTERN_MODULE (CaptureSource, RCTEventEmitter)

RCT_EXTERN_METHOD(startDiscovery)
RCT_EXTERN_METHOD(stopDiscovery)

RCT_EXTERN_METHOD(connect:(NSString *)cameraID
                  resolver:(RCTPromiseResolveBlock)resolve
                  rejecter:(RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(disconnect:(NSString *)cameraID
                  resolver:(RCTPromiseResolveBlock)resolve
                  rejecter:(RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(listItems:(NSString *)cameraID
                  after:(NSString *)after
                  limit:(nonnull NSNumber *)limit
                  resolver:(RCTPromiseResolveBlock)resolve
                  rejecter:(RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(fetchPreview:(NSString *)cameraID
                  itemID:(NSString *)itemID
                  resolver:(RCTPromiseResolveBlock)resolve
                  rejecter:(RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(fetchOriginal:(NSString *)cameraID
                  itemID:(NSString *)itemID
                  resolver:(RCTPromiseResolveBlock)resolve
                  rejecter:(RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(triggerShutter:(NSString *)cameraID
                  resolver:(RCTPromiseResolveBlock)resolve
                  rejecter:(RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(startLiveView:(NSString *)cameraID
                  resolver:(RCTPromiseResolveBlock)resolve
                  rejecter:(RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(stopLiveView:(NSString *)cameraID
                  resolver:(RCTPromiseResolveBlock)resolve
                  rejecter:(RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(readSettings:(NSString *)cameraID
                  resolver:(RCTPromiseResolveBlock)resolve
                  rejecter:(RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(writeSetting:(NSString *)cameraID
                  key:(NSString *)key
                  valueJSON:(NSString *)valueJSON
                  resolver:(RCTPromiseResolveBlock)resolve
                  rejecter:(RCTPromiseRejectBlock)reject)

@end
