const { getDefaultConfig, mergeConfig } = require('@react-native/metro-config');

/**
 * Cấu hình Metro.
 *
 * `.web.tsx` bị LOẠI khỏi sourceExts ở đây: các file đó chỉ dành cho bản xem
 * trước trên trình duyệt. Nếu Metro nhặt chúng, bản chạy trên máy sẽ dùng
 * GradedImage.web.tsx — tức là màu xấp xỉ bằng CSS filter thay vì LUT hald chạy
 * trong shader Skia. Lỗi đó không làm gãy build và rất khó nhận ra, vì ảnh vẫn
 * "có màu", chỉ là sai màu.
 */
const config = {
  resolver: {
    sourceExts: ['ts', 'tsx', 'js', 'jsx', 'json'],
  },
};

module.exports = mergeConfig(getDefaultConfig(__dirname), config);
