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

const CHROME = 'C:/Program Files/Google/Chrome/Application/chrome.exe';
const BASE = 'http://127.0.0.1:5199';
const SCREENS = ['signin', 'sessions', 'tether', 'photo', 'storage', 'client'];

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
