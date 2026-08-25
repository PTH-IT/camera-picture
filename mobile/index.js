/**
 * Điểm vào của ứng dụng trên máy thật.
 *
 * Bản xem trước trên trình duyệt KHÔNG đi qua file này — nó mount App trực tiếp
 * (xem preview/src/main.tsx). Giữ file này mỏng để hai đường không trôi lệch:
 * mọi thứ có ý nghĩa phải nằm trong src/App.tsx.
 */
import { AppRegistry } from 'react-native';
import { App } from './src/App';
import { name as appName } from './app.json';

AppRegistry.registerComponent(appName, () => App);
