#
# Tầng capture đóng gói thành pod cục bộ.
#
# Vì sao là pod chứ không phải kéo file vào Xcode bằng tay: kéo tay có nghĩa là
# dự án chỉ build được trên máy của người đã kéo, và không ai kiểm chứng được
# bước đó trong pull request. Là pod thì `pod install` dựng lại y hệt trên mọi
# máy, và CI chạy được.
#
# Pod này KHÔNG được autolink (autolink chỉ quét node_modules), nên Podfile phải
# khai báo nó tường minh:
#
#   pod 'CaptureSource', :path => './CaptureSource'
#
# `bootstrap.sh` chèn dòng đó vào Podfile do template sinh ra.
#
Pod::Spec.new do |s|
  s.name         = 'CaptureSource'
  s.version      = '0.1.0'
  s.summary      = 'Cầu nối React Native ↔ SDK máy ảnh (tether)'
  s.homepage     = 'https://github.com/PTH-IT/camera-picture'
  s.authors      = 'PTH-IT'
  s.license      = { :type => 'Proprietary',
                     :text => 'Copyright (c) 2026 PTH-IT. All rights reserved.' }
  s.source       = { :git => 'https://github.com/PTH-IT/camera-picture.git' }

  # `min_ios_version_supported` do react_native_pods.rb định nghĩa và là nguồn
  # sự thật khi pod được cài từ Podfile của React Native. Bản dự phòng chỉ dùng
  # khi ai đó lint podspec này một mình, ngoài ngữ cảnh đó.
  s.platforms    = { :ios => defined?(min_ios_version_supported) ? min_ios_version_supported : '15.1' }
  s.swift_version = '5.0'

  # Chỉ file ở thư mục này, KHÔNG đệ quy: README.md và podspec không phải mã
  # nguồn, và thư mục con trong tương lai phải được thêm vào đây một cách có ý
  # thức chứ không lọt vào build một cách tình cờ.
  s.source_files = '*.{swift,h,m,mm}'

  s.dependency 'React-Core'

  # CascableCore KHÔNG khai ở đây. Nó là SDK thương mại phân phối qua Swift
  # Package Manager (Cascable/cascablecore-distribution) và không được commit
  # vào repo — thêm nó vào target app trong Xcode sau khi có license.
  # Xem docs/adr/0001-capture-strategy.md.
end
