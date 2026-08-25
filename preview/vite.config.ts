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
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      'react-native': 'react-native-web',
      '@app': new URL('../mobile/src', import.meta.url).pathname,
    },
    extensions: ['.web.tsx', '.web.ts', '.tsx', '.ts', '.jsx', '.js'],
  },
  server: { port: 5199, host: '127.0.0.1' },
});
