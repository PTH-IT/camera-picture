// Chụp màn hình bản xem trước.
//
// Dùng puppeteer-core thay vì cờ --screenshot của Chrome vì một lý do cụ thể:
// trên Windows, Chrome có chiều rộng cửa sổ tối thiểu, nên --window-size=375
// vẫn cho innerWidth=504. Hậu quả là ứng dụng bố cục ở 504px trong khi ảnh chỉ
// chụp 375px, và mọi thứ căn theo mép phải trông như bị cắt — một lỗi của công
// cụ chụp rất dễ bị nhầm thành lỗi giao diện.
//
// CDP đặt được viewport chính xác, nên useWindowDimensions trả đúng giá trị và
// ảnh chụp phản ánh đúng bố cục trên máy thật.
import puppeteer from 'puppeteer-core';

// Đường dẫn Chrome theo hệ điều hành, ghi đè được bằng CHROME_PATH.
//
// Dự án được phát triển trên Windows nhưng build iOS thì bắt buộc macOS, nên
// công cụ chụp phải chạy được ở cả hai. Đường dẫn cứng một hệ khiến người ngồi
// máy kia phải sửa file rồi lỡ tay commit bản sửa đó lên.
const CHROME_BY_PLATFORM = {
  win32: 'C:/Program Files/Google/Chrome/Application/chrome.exe',
  darwin: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  linux: '/usr/bin/google-chrome',
};
const CHROME = process.env.CHROME_PATH ?? CHROME_BY_PLATFORM[process.platform];
if (!CHROME) {
  console.error(`Không biết Chrome nằm đâu trên ${process.platform}. Đặt CHROME_PATH.`);
  process.exit(1);
}
// Cổng đọc từ môi trường: nếu một tiến trình vite cũ còn giữ 5199, vite tự
// nhảy sang cổng khác và ảnh chụp sẽ là của server cũ với cache hỏng.
const BASE = process.env.PREVIEW_URL ?? 'http://127.0.0.1:5199';
const SCREENS = [
  'signin',
  'sessions',
  'tether',
  'tether-empty',
  'tether-fulldownload',
  'photo',
  'storage',
  'client',
];

const browser = await puppeteer.launch({
  executablePath: CHROME,
  headless: true,
  args: ['--no-sandbox', '--disable-gpu'],
});

const page = await browser.newPage();
// iPhone 15 ở kích thước điểm. deviceScaleFactor 2 để ảnh sắc nét khi xem lại.
await page.setViewport({ width: 393, height: 852, deviceScaleFactor: 2 });

for (const name of SCREENS) {
  await page.goto(`${BASE}/#${name}`, { waitUntil: 'networkidle0' });

  // Chờ ảnh mẫu tải xong rồi mới chụp, nếu không lưới sẽ có ô trống.
  await page.evaluate(
    () =>
      Promise.all(
        [...document.images]
          .filter(i => !i.complete)
          .map(i => new Promise(r => { i.onload = i.onerror = r; })),
      ),
  );
  await new Promise(r => setTimeout(r, 400));

  await page.screenshot({ path: `shots/${name}.png` });

  const w = await page.evaluate(() => window.innerWidth);
  const overflow = await page.evaluate(() => document.body.scrollWidth > window.innerWidth);
  console.log(`${name.padEnd(10)} viewport=${w}${overflow ? '  TRÀN NGANG' : ''}`);
}

await browser.close();
