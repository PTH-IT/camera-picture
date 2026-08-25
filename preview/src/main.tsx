import { StrictMode, useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { AppRegistry } from 'react-native';
import { Harness } from './Harness';

// react-native-web cần đăng ký ứng dụng rồi mới chạy được, giống trên máy thật.
AppRegistry.registerComponent('preview', () => Harness);

function Root() {
  const [, force] = useState(0);
  // Harness chọn màn hình theo hash để chụp màn hình từng cái được mà không cần
  // bấm qua từng bước.
  useEffect(() => {
    const on = () => force(n => n + 1);
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []);
  return <Harness />;
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Root />
  </StrictMode>,
);
