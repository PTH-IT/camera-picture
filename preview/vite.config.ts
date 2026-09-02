// `URL` lấy từ node:url chứ không dùng bản toàn cục của DOM: hai kiểu này khác
// nhau về khai báo iterator, và `fileURLToPath` chỉ nhận bản của Node.
import { URL, fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Bản xem trước trên trình duyệt cho giao diện mobile.
//
// Tồn tại vì máy phát triển là Windows, không có iOS Simulator — nếu không có
// nó thì cách duy nhất để "kiểm tra" giao diện là đọc code và tin tưởng. Ở đây
// màn hình chạy thật, bấm được, và chụp màn hình được.
//
// KHÔNG phải một phiên bản web của sản phẩm. Nó nạp thẳng cùng bộ mã nguồn ở
// mobile/src, chỉ thay react-native bằng react-native-web. Màn hình nào chạy
// lệch ở đây so với trên máy thật là dấu hiệu của lỗi, không phải chuyện bình thường.
// Đường dẫn TUYỆT ĐỐI, không phải tên gói.
//
// Thư viện native nằm trong mobile/node_modules (react-native-safe-area-context
// chẳng hạn) cũng import 'react-native'. Với alias dạng tên gói, Vite rewrite
// thành 'react-native-web' rồi đi tìm từ thư mục của CHÍNH thư viện đó — nơi
// không có react-native-web — và build chết bằng thông báo
// "Could not load react-native-web" chỉ vào một file trong thư viện, không chỉ
// vào nguyên nhân. Đường dẫn tuyệt đối thì tìm ở đâu cũng ra.
const reactNativeWeb = fileURLToPath(new URL('./node_modules/react-native-web', import.meta.url));

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: [
      // Chỉ khớp CHÍNH XÁC 'react-native'. Dùng chuỗi thì Vite khớp cả tiền tố
      // và mọi deep import cũng bị nối vào đường dẫn tuyệt đối này.
      { find: /^react-native$/, replacement: reactNativeWeb },
      { find: /^react-native\//, replacement: 'react-native-web/' },
      { find: '@app', replacement: fileURLToPath(new URL('../mobile/src', import.meta.url)) },
    ],
    // `.web.js` phải có mặt, không chỉ `.web.tsx`: thư viện đã build sẵn (như
    // react-native-safe-area-context) ship bản web dưới dạng .web.js bên cạnh
    // bản native. Thiếu đuôi này thì Vite lấy bản native, bản đó import spec
    // codegen của Fabric, và build chết ở một file trong node_modules.
    extensions: ['.web.tsx', '.web.ts', '.web.jsx', '.web.js', '.tsx', '.ts', '.jsx', '.js'],
  },
  // Bước "tối ưu phụ thuộc" của Vite gói sẵn node_modules bằng một trình bundle
  // riêng KHÔNG áp dụng `resolve.extensions`, nên nó lấy bản native của thư viện
  // và chết ở import spec Fabric. Loại nó ra khỏi bước đó để đi đường transform
  // thường — chậm hơn vài chục mili giây lúc khởi động, đổi lại chạy được.
  optimizeDeps: { exclude: ['react-native-safe-area-context'] },

  server: { port: 5199, host: '127.0.0.1' },
});
