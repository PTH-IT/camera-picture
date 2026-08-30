/**
 * Kiểm thử tích hợp cho ApiClient, chạy với backend THẬT.
 *
 * Không mock fetch. Lý do: thứ dễ sai nhất ở tầng client không phải logic
 * JavaScript mà là chỗ nó khớp (hoặc không khớp) với server — tên trường JSON,
 * mã lỗi, mã trạng thái HTTP, ngữ nghĩa con trỏ đồng bộ. Mock chỉ khẳng định lại
 * giả định của chính mình, và giả định đó mới là chỗ có lỗi.
 *
 * Chạy:
 *   cd backend && go run ./cmd/api            (với DATABASE_URL trỏ Postgres)
 *   cd preview && npx tsx itest.ts
 */

import { ApiClient, ApiError, pullAllChanges, type ImageInput } from '@app/api/client';

const BASE = process.env.API_BASE ?? 'http://127.0.0.1:8420';

let passed = 0;
let failed = 0;

function check(name: string, ok: boolean, detail = '') {
  if (ok) {
    passed++;
    console.log(`  PASS  ${name}`);
  } else {
    failed++;
    console.log(`  FAIL  ${name}${detail ? ' — ' + detail : ''}`);
  }
}

async function expectError(name: string, fn: () => Promise<unknown>, code: string) {
  try {
    await fn();
    check(name, false, `không ném lỗi, mong đợi ${code}`);
  } catch (e) {
    const got = e instanceof ApiError ? e.code : String(e);
    check(name, got === code, `nhận ${got}, mong đợi ${code}`);
  }
}

function newClient() {
  return new ApiClient({ baseUrl: BASE });
}

function uniqueEmail() {
  return `it-${Date.now()}-${Math.floor(Math.random() * 1e6)}@example.test`;
}

function shots(seq: number, n: number): ImageInput[] {
  const base = Date.UTC(2026, 7, 26, 9, 0, 0);
  return Array.from({ length: n }, (_, i) => ({
    clientId: `S${seq}_${String(i).padStart(5, '0')}`,
    filename: `DSC_${4000 + i}.NEF`,
    format: 'NEF' as const,
    byteSize: 55 * 1024 * 1024,
    capturedAt: new Date(base + i * 1000).toISOString(),
    isRaw: true,
  }));
}

async function main() {
  console.log(`Kiểm thử tích hợp ApiClient với ${BASE}\n`);

  // --- xác thực ---
  console.log('Xác thực');
  const c = newClient();
  const email = uniqueEmail();

  const signUp = await c.signUp(email, 'mat-khau-du-dai-12', 'Người Dùng');
  check('đăng ký trả về token và user', !!signUp.token && !!signUp.user.id);
  check('token được lưu vào client', c.tokens.get() === signUp.token);
  check('email chuẩn hoá chữ thường', signUp.user.email === email.toLowerCase());

  const me = await c.me();
  check('/v1/me trả đúng người dùng', me.id === signUp.user.id);

  await expectError(
    'sai mật khẩu trả unauthorized',
    () => newClient().signIn(email, 'sai-mat-khau-dai'),
    'unauthorized',
  );
  await expectError(
    'email chưa đăng ký cũng trả unauthorized',
    () => newClient().signIn(uniqueEmail(), 'mat-khau-du-dai-12'),
    'unauthorized',
  );
  await expectError(
    'mật khẩu quá ngắn bị từ chối',
    () => newClient().signUp(uniqueEmail(), 'ngan', 'X'),
    'invalid_input',
  );
  await expectError(
    'email trùng bị từ chối',
    () => newClient().signUp(email, 'mat-khau-du-dai-12'),
    'conflict',
  );
  await expectError(
    'OIDC thiếu nonce bị từ chối',
    () =>
      newClient().signInWithOIDC({ provider: 'google', idToken: 'gia.mao', nonce: '' }),
    'invalid_input',
  );

  // --- buổi chụp và đồng bộ ---
  console.log('\nBuổi chụp và đồng bộ');
  const session = await c.createSession('Kiểm thử tích hợp', 'Khách');
  check('tạo buổi chụp trả về id', !!session.ID);
  check('id là UUID v7', /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-/.test(session.ID));

  const listed = await c.listSessions();
  const mine = listed.find(x => x.ID === session.ID);
  check('liệt kê thấy buổi vừa tạo', !!mine);
  check('buổi mới đếm ra 0 ảnh', mine?.ImageCount === 0, `${mine?.ImageCount}`);

  const batch1 = await c.batchImages(session.ID, shots(1, 40));
  check('đẩy 40 ảnh: created=40', batch1.created === 40, `created=${batch1.created}`);
  check('trả về ánh xạ clientId -> id', Object.keys(batch1.ids).length === 40);

  const batch2 = await c.batchImages(session.ID, shots(1, 40));
  check('gửi lại y hệt là no-op', batch2.created === 0 && batch2.updated === 0);
  check(
    'retry mù KHÔNG đẩy revision lên',
    batch2.revision === batch1.revision,
    `${batch1.revision} -> ${batch2.revision}`,
  );

  const listedAfter = await c.listSessions();
  check(
    'máy chủ đếm ảnh, không phải client',
    listedAfter.find(x => x.ID === session.ID)?.ImageCount === 40,
    `${listedAfter.find(x => x.ID === session.ID)?.ImageCount}`,
  );

  // Đồng bộ delta qua nhiều trang, dùng đúng hàm mà app sẽ dùng.
  const pull = await pullAllChanges(c, session.ID, 0, { limit: 7 });
  check('kéo hết delta: đủ 40 ảnh', pull.images.length === 40, `nhận ${pull.images.length}`);
  check('phân trang chạy nhiều vòng', pull.rounds > 1, `${pull.rounds} vòng`);
  const ids = new Set(pull.images.map(i => i.id));
  check('không bản ghi nào lặp lại', ids.size === 40, `${ids.size} id duy nhất`);
  check(
    'ảnh mới chưa có asset (vẫn trên thẻ)',
    pull.images.every(i => !i.assets || Object.keys(i.assets).length === 0),
  );

  const empty = await pullAllChanges(c, session.ID, pull.revision);
  check('đồng bộ lần hai không kéo lại gì', empty.images.length === 0 && empty.edits.length === 0);

  // --- chỉnh sửa ---
  console.log('\nChỉnh sửa');
  const firstId = batch1.ids['S1_00000']!;
  const edit = await c.putEdit(firstId, {
    rating: 5,
    flagged: true,
    rejected: false,
    deviceId: 'itest',
    overrides: { exposure: 0.2 },
  });
  check('ghi chỉnh sửa trả về bản ghi', edit.imageId === firstId && edit.rating === 5);

  const afterEdit = await pullAllChanges(c, session.ID, empty.revision);
  check('chỉnh sửa xuất hiện trong delta', afterEdit.edits.length === 1);
  check('overrides đi qua nguyên vẹn', afterEdit.edits[0]?.overrides?.exposure === 0.2);

  // --- asset ---
  console.log('\nAsset');
  await c.confirmAsset(firstId, {
    tier: 'preview',
    storageKey: `users/x/preview/${firstId}.jpg`,
    byteSize: 1024 * 400,
    width: 2048,
    height: 1365,
  });
  const afterAsset = await pullAllChanges(c, session.ID, afterEdit.revision);
  const withAsset = afterAsset.images.find(i => i.id === firstId);
  check('asset xuất hiện trong delta', !!withAsset?.assets?.preview);
  check('kích thước asset đúng', withAsset?.assets?.preview?.width === 2048);

  // --- xoá mềm ---
  console.log('\nXoá mềm');
  const toDelete = batch1.ids['S1_00001']!;
  await c.deleteImage(toDelete);
  const afterDelete = await pullAllChanges(c, session.ID, afterAsset.revision);
  const deleted = afterDelete.images.find(i => i.id === toDelete);
  check('bản ghi xoá tới được client', deleted?.deleted === true);

  // --- phân quyền ---
  console.log('\nPhân quyền');
  const attacker = newClient();
  await attacker.signUp(uniqueEmail(), 'mat-khau-du-dai-12');
  await expectError(
    'không đọc được buổi chụp của người khác',
    () => attacker.changes(session.ID, 0),
    'not_found',
  );
  await expectError(
    'không sửa được ảnh của người khác',
    () => attacker.putEdit(firstId, { rating: 1, flagged: false, rejected: false }),
    'not_found',
  );
  const attackerList = await attacker.listSessions();
  check(
    'không thấy buổi chụp của người khác trong danh sách',
    attackerList.every(x => x.ID !== session.ID),
    `${attackerList.length} buổi`,
  );

  await expectError(
    'không có token thì bị từ chối',
    () => new ApiClient({ baseUrl: BASE }).me(),
    'unauthorized',
  );

  // --- lưu trữ ---
  console.log('\nLưu trữ');
  const opts = await c.storageOptions();
  check('có ít nhất một nhà cung cấp', opts.options.length > 0);
  check('mặc định là device', opts.selected === 'device', `selected=${opts.selected}`);

  await c.selectStorage('managed');
  const usage = await c.storageUsage();
  check('hạn mức mặc định 2 GiB', usage.limitBytes === 2 * 1024 ** 3, `${usage.limitBytes}`);
  check('managed cưỡng chế hạn mức', usage.enforced === true);

  await expectError(
    'hoá đơn tự chế bị từ chối',
    () => c.redeemPurchase('apple', 'hoa-don-tu-che'),
    'invalid_input',
  );

  // Drive chưa cấu hình trên máy chủ này — phải là 501 not_configured, không
  // phải lỗi máy chủ, để giao diện ẩn nút thay vì báo lỗi.
  await expectError('Drive chưa cấu hình trả not_configured', () => c.driveAuthUrl(), 'not_configured');

  // --- máy ảnh ---
  console.log('\nMáy ảnh');
  const cam = await c.registerCamera({
    manufacturer: 'Nikon',
    model: 'Z 8',
    firmware: '2.10',
    transport: 'wifi',
    capabilities: ['remoteShutter', 'previewWithoutFullDownload'],
  });
  check('đăng ký máy ảnh trả về id', !!cam.ID);
  check('id là UUID', /^[0-9a-f]{8}-[0-9a-f]{4}-/.test(cam.ID), cam.ID);

  const again = await c.registerCamera({ manufacturer: 'Nikon', model: 'Z 8', transport: 'usb' });
  check('cắm lại cùng thân máy không sinh id mới', again.ID === cam.ID);
  check('đường truyền cập nhật theo lần gần nhất', again.Transport === 'usb', again.Transport);

  const tagged = shots(9, 1).map(x => ({ ...x, cameraId: cam.ID }));
  const taggedRes = await c.batchImages(session.ID, tagged);
  const taggedId = taggedRes.ids['S9_00000']!;
  const taggedPull = await pullAllChanges(c, session.ID, 0);
  check(
    'cameraId quay về nguyên vẹn trong delta',
    taggedPull.images.find(i => i.id === taggedId)?.cameraId === cam.ID,
  );

  await expectError(
    'ảnh gắn máy ảnh không có thật bị từ chối',
    () =>
      c.batchImages(session.ID, [
        { ...shots(9, 1)[0]!, clientId: 'S9_BOGUS', cameraId: '00000000-0000-0000-0000-000000000000' },
      ]),
    'invalid_input',
  );

  const other = newClient();
  await other.signUp(uniqueEmail(), 'mat-khau-du-dai-12');
  const otherSession = await other.createSession('Buổi khác');
  await expectError(
    'không gắn được máy ảnh của người khác vào ảnh của mình',
    () =>
      other.batchImages(otherSession.ID, [
        { ...shots(9, 1)[0]!, clientId: 'S9_STOLEN', cameraId: cam.ID },
      ]),
    'invalid_input',
  );

  // --- đăng xuất ---
  console.log('\nĐăng xuất');
  await c.signOut();
  check('token bị xoá khỏi client', c.tokens.get() === null);
  await expectError('token sau đăng xuất không dùng được', () => c.me(), 'unauthorized');

  console.log(`\n${passed} đạt, ${failed} hỏng`);
  process.exit(failed === 0 ? 0 : 1);
}

main().catch(e => {
  console.error('\nLỗi không mong đợi:', e);
  process.exit(1);
});
