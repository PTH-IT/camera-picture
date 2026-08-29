// Package storetest là bộ test tuân thủ dùng chung cho mọi bản triển khai
// store.Store.
//
// Vì sao cần: dự án có hai bản triển khai. Bản in-memory dùng trong unit test của
// tầng trên (nhanh, không cần Postgres), bản pg dùng trong production. Nếu chúng
// hành xử khác nhau, mọi test đều xanh trong khi production sai — kiểu lỗi tệ
// nhất vì không có tín hiệu nào cho tới khi khách hàng báo.
//
// Đặt hợp đồng vào một chỗ và bắt cả hai bản đi qua nó là cách rẻ nhất để chặn
// điều đó.
package storetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hauph/camera/backend/internal/protocol"
	"github.com/hauph/camera/backend/internal/store"
)

// Factory tạo một store trống cho mỗi test.
type Factory func(t *testing.T) store.Store

// Run chạy toàn bộ bộ test tuân thủ.
func Run(t *testing.T, newStore Factory) {
	t.Helper()

	tests := []struct {
		name string
		fn   func(*testing.T, store.Store)
	}{
		{"BatchUpsertIsIdempotent", testBatchUpsertIsIdempotent},
		{"DeltaSyncLosesNothing", testDeltaSyncLosesNothing},
		{"ChangesIsIncremental", testChangesIsIncremental},
		{"SoftDeletePropagates", testSoftDeletePropagates},
		{"ConfirmAssetBumpsRevision", testConfirmAssetBumpsRevision},
		{"ImageWithoutAssetsIsNormal", testImageWithoutAssetsIsNormal},
		{"UnknownSession", testUnknownSession},
		{"EditsAndImagesShareRevisionSpace", testEditsAndImagesShareRevisionSpace},
		{"EmptyClientIDRejected", testEmptyClientIDRejected},
		{"ListSessionsIsPerUser", testListSessionsIsPerUser},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, newStore(t))
		})
	}
}

func mkSession(t *testing.T, s store.Store) string {
	t.Helper()
	sess, err := s.CreateSession(context.Background(), newUserID(t, s), "Đám cưới", "Khách A",
		time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return sess.ID
}

// userID được cấp bởi factory nếu bản triển khai cần khoá ngoại thật (Postgres
// có ràng buộc REFERENCES users). Bản in-memory bỏ qua.
type userProvider interface {
	TestUserID(t *testing.T) string
}

func newUserID(t *testing.T, s store.Store) string {
	if up, ok := s.(userProvider); ok {
		return up.TestUserID(t)
	}
	return "user-1"
}

func mkImages(seq, n int) []protocol.ImageInput {
	out := make([]protocol.ImageInput, n)
	base := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	for i := range out {
		out[i] = protocol.ImageInput{
			ClientID:   fmt.Sprintf("DSC_%06d", seq*100000+i),
			Filename:   fmt.Sprintf("DSC_%04d.NEF", i),
			Format:     protocol.FormatNEF,
			ByteSize:   55 * 1024 * 1024,
			CapturedAt: base.Add(time.Duration(i) * time.Second).UTC(),
			IsRaw:      true,
		}
	}
	return out
}

// testBatchUpsertIsIdempotent: buổi chụp thật hay rớt mạng và client buộc phải
// gửi lại lô đã gửi mà không biết lần trước có tới nơi không.
func testBatchUpsertIsIdempotent(t *testing.T, s store.Store) {
	ctx := context.Background()
	sid := mkSession(t, s)
	in := mkImages(0, 50)

	first, err := s.BatchUpsertImages(ctx, sid, in)
	if err != nil {
		t.Fatalf("lần 1: %v", err)
	}
	if first.Created != 50 || first.Updated != 0 {
		t.Fatalf("lần 1: created=%d updated=%d, muốn 50/0", first.Created, first.Updated)
	}

	second, err := s.BatchUpsertImages(ctx, sid, in)
	if err != nil {
		t.Fatalf("lần 2: %v", err)
	}
	if second.Created != 0 || second.Updated != 0 {
		t.Errorf("lần 2: created=%d updated=%d, muốn 0/0 — gửi lại phải là no-op",
			second.Created, second.Updated)
	}
	// Điểm quan trọng nhất: retry mù KHÔNG được đẩy revision lên. Nếu có, mọi
	// client khác phải đồng bộ lại vô ích, tạo vòng lặp tự nuôi tốn pin và băng thông.
	if second.Revision != first.Revision {
		t.Errorf("revision nhảy từ %d lên %d khi retry mù", first.Revision, second.Revision)
	}

	// Thay đổi thật thì phải được ghi nhận.
	in[3].Filename = "DSC_0003_sua.NEF"
	third, err := s.BatchUpsertImages(ctx, sid, in)
	if err != nil {
		t.Fatalf("lần 3: %v", err)
	}
	if third.Updated != 1 {
		t.Errorf("updated = %d, muốn 1", third.Updated)
	}
	if third.Revision <= second.Revision {
		t.Errorf("revision không tăng sau thay đổi thật: %d -> %d", second.Revision, third.Revision)
	}

	// Và id phải ổn định qua các lần gửi lại — nếu không, client sẽ nghĩ đó là
	// ảnh mới và tạo bản trùng phía nó.
	if first.IDs["DSC_000000"] != third.IDs["DSC_000000"] {
		t.Errorf("id đổi giữa các lần upsert: %q -> %q",
			first.IDs["DSC_000000"], third.IDs["DSC_000000"])
	}
}

// testDeltaSyncLosesNothing là test quan trọng nhất của hợp đồng.
//
// Mô phỏng client đồng bộ qua NHIỀU TRANG và khẳng định mọi bản ghi đến đúng một
// lần. Nếu nhiều bản ghi dùng chung một revision, client lấy nửa nhóm, đặt con
// trỏ bằng revision đó, và nửa còn lại vĩnh viễn không thoả "> since". Ảnh biến
// mất mà không có lỗi nào.
func testDeltaSyncLosesNothing(t *testing.T, s store.Store) {
	ctx := context.Background()
	sid := mkSession(t, s)

	const total = 237
	res, err := s.BatchUpsertImages(ctx, sid, mkImages(0, total))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Xen kẽ chỉnh sửa để ảnh và edit trộn lẫn trong dòng revision.
	edited := 0
	for cid, id := range res.IDs {
		if edited >= 40 {
			break
		}
		if _, err := s.PutEdit(ctx, id, protocol.PutEditRequest{Rating: 4, DeviceID: "iphone"}); err != nil {
			t.Fatalf("PutEdit %s: %v", cid, err)
		}
		edited++
	}

	seenImages := map[string]int{}
	seenEdits := map[string]int{}
	cursor := int64(0)
	rounds := 0

	for {
		rounds++
		if rounds > 200 {
			t.Fatal("không hội tụ sau 200 vòng — nhiều khả năng con trỏ không tiến")
		}
		resp, err := s.Changes(ctx, sid, cursor, 25)
		if err != nil {
			t.Fatalf("Changes: %v", err)
		}
		for _, img := range resp.Images {
			seenImages[img.ID]++
		}
		for _, ed := range resp.Edits {
			seenEdits[ed.ImageID]++
		}
		if resp.Revision <= cursor && (len(resp.Images) > 0 || len(resp.Edits) > 0) {
			t.Fatalf("con trỏ không tiến: %d -> %d nhưng vẫn có bản ghi", cursor, resp.Revision)
		}
		cursor = resp.Revision
		if !resp.HasMore {
			break
		}
	}

	if len(seenImages) != total {
		t.Errorf("thấy %d ảnh, muốn %d — ĐÃ MẤT BẢN GHI", len(seenImages), total)
	}
	for id, n := range seenImages {
		if n != 1 {
			t.Errorf("ảnh %s xuất hiện %d lần, muốn đúng 1", id, n)
		}
	}
	if len(seenEdits) != edited {
		t.Errorf("thấy %d edit, muốn %d", len(seenEdits), edited)
	}
	t.Logf("hội tụ sau %d vòng, con trỏ cuối = %d", rounds, cursor)
}

func testChangesIsIncremental(t *testing.T, s store.Store) {
	ctx := context.Background()
	sid := mkSession(t, s)

	if _, err := s.BatchUpsertImages(ctx, sid, mkImages(0, 100)); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	first, err := s.Changes(ctx, sid, 0, 500)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(first.Images) != 100 {
		t.Fatalf("lần đầu lấy %d ảnh, muốn 100", len(first.Images))
	}

	second, err := s.Changes(ctx, sid, first.Revision, 500)
	if err != nil {
		t.Fatalf("Changes lần 2: %v", err)
	}
	if len(second.Images) != 0 || len(second.Edits) != 0 {
		t.Errorf("lần 2 trả về %d ảnh + %d edit, muốn 0 — không được kéo lại",
			len(second.Images), len(second.Edits))
	}

	if _, err := s.BatchUpsertImages(ctx, sid, mkImages(1, 1)); err != nil {
		t.Fatalf("upsert lẻ: %v", err)
	}
	third, err := s.Changes(ctx, sid, second.Revision, 500)
	if err != nil {
		t.Fatalf("Changes lần 3: %v", err)
	}
	if len(third.Images) != 1 {
		t.Errorf("lần 3 trả về %d ảnh, muốn đúng 1", len(third.Images))
	}
}

func testSoftDeletePropagates(t *testing.T, s store.Store) {
	ctx := context.Background()
	sid := mkSession(t, s)

	res, err := s.BatchUpsertImages(ctx, sid, mkImages(0, 3))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	synced, _ := s.Changes(ctx, sid, 0, 500)

	target := res.IDs["DSC_000001"]
	if err := s.SoftDeleteImage(ctx, target); err != nil {
		t.Fatalf("SoftDeleteImage: %v", err)
	}

	after, err := s.Changes(ctx, sid, synced.Revision, 500)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(after.Images) != 1 {
		t.Fatalf("trả về %d ảnh, muốn 1", len(after.Images))
	}
	if after.Images[0].ID != target || !after.Images[0].Deleted {
		t.Errorf("bản ghi xoá không tới được client: %+v", after.Images[0])
	}

	// Xoá lần hai không được cấp revision mới — nếu có, client sẽ đồng bộ lại
	// một thay đổi không tồn tại.
	before := after.Revision
	if err := s.SoftDeleteImage(ctx, target); err != nil {
		t.Fatalf("xoá lần 2: %v", err)
	}
	again, _ := s.Changes(ctx, sid, before, 500)
	if len(again.Images) != 0 {
		t.Errorf("xoá lần 2 tạo thêm thay đổi: %d ảnh", len(again.Images))
	}
}

func testConfirmAssetBumpsRevision(t *testing.T, s store.Store) {
	ctx := context.Background()
	sid := mkSession(t, s)

	res, err := s.BatchUpsertImages(ctx, sid, mkImages(0, 2))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	synced, _ := s.Changes(ctx, sid, 0, 500)

	target := res.IDs["DSC_000000"]
	err = s.ConfirmAsset(ctx, target, protocol.ConfirmAssetRequest{
		Tier:       protocol.TierOriginal,
		StorageKey: "s3://bucket/orig/DSC_0000.NEF",
		ByteSize:   55 * 1024 * 1024,
		Width:      8256,
		Height:     5504,
	})
	if err != nil {
		t.Fatalf("ConfirmAsset: %v", err)
	}

	after, _ := s.Changes(ctx, sid, synced.Revision, 500)
	if len(after.Images) != 1 {
		t.Fatalf("trả về %d ảnh, muốn 1", len(after.Images))
	}
	asset, ok := after.Images[0].Assets[protocol.TierOriginal]
	if !ok {
		t.Fatalf("asset original không có trong bản đồng bộ: %+v", after.Images[0].Assets)
	}
	if asset.StorageKey != "s3://bucket/orig/DSC_0000.NEF" || asset.Width != 8256 {
		t.Errorf("asset sai: %+v", asset)
	}
}

// testImageWithoutAssetsIsNormal ghi lại một bất biến của kiến trúc: phần lớn ảnh
// KHÔNG BAO GIỜ lên server. Nếu ai đó thêm ràng buộc "ảnh phải có asset", test
// này vỡ và nhắc họ đọc lại ADR 0001.
func testImageWithoutAssetsIsNormal(t *testing.T, s store.Store) {
	ctx := context.Background()
	sid := mkSession(t, s)

	res, err := s.BatchUpsertImages(ctx, sid, mkImages(0, 1))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	img, err := s.GetImage(ctx, res.IDs["DSC_000000"])
	if err != nil {
		t.Fatalf("GetImage: %v", err)
	}
	if len(img.Assets) != 0 {
		t.Errorf("ảnh mới phải không có asset nào, có %d", len(img.Assets))
	}
}

func testUnknownSession(t *testing.T, s store.Store) {
	ctx := context.Background()

	// Dùng UUID hợp lệ nhưng không tồn tại: bản pg sẽ từ chối chuỗi không phải
	// UUID ở tầng kiểu, và ta muốn kiểm tra nhánh "không tìm thấy" thật sự.
	const missing = "00000000-0000-7000-8000-000000000000"

	if _, err := s.Changes(ctx, missing, 0, 10); err != store.ErrNotFound {
		t.Errorf("Changes trả %v, muốn ErrNotFound", err)
	}
	if _, err := s.BatchUpsertImages(ctx, missing, mkImages(0, 1)); err != store.ErrNotFound {
		t.Errorf("BatchUpsertImages trả %v, muốn ErrNotFound", err)
	}
	if _, err := s.GetImage(ctx, missing); err != store.ErrNotFound {
		t.Errorf("GetImage trả %v, muốn ErrNotFound", err)
	}
}

// testEditsAndImagesShareRevisionSpace: cả hai dùng chung bộ đếm revision của
// buổi chụp. Nếu tách hai bộ đếm, client sẽ phải giữ hai con trỏ và rất dễ lệch.
func testEditsAndImagesShareRevisionSpace(t *testing.T, s store.Store) {
	ctx := context.Background()
	sid := mkSession(t, s)

	res, err := s.BatchUpsertImages(ctx, sid, mkImages(0, 3))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	id := res.IDs["DSC_000000"]

	edit, err := s.PutEdit(ctx, id, protocol.PutEditRequest{Rating: 5})
	if err != nil {
		t.Fatalf("PutEdit: %v", err)
	}
	if edit.Revision <= res.Revision {
		t.Errorf("revision của edit (%d) không lớn hơn revision cuối của ảnh (%d) — hai bộ đếm đang tách rời",
			edit.Revision, res.Revision)
	}

	all, _ := s.Changes(ctx, sid, 0, 500)
	var maxRev int64
	for _, i := range all.Images {
		if i.Revision > maxRev {
			maxRev = i.Revision
		}
	}
	for _, e := range all.Edits {
		if e.Revision > maxRev {
			maxRev = e.Revision
		}
	}
	if all.Revision != maxRev {
		t.Errorf("Revision trả về (%d) không bằng revision lớn nhất trong kết quả (%d)",
			all.Revision, maxRev)
	}
}

func testEmptyClientIDRejected(t *testing.T, s store.Store) {
	ctx := context.Background()
	sid := mkSession(t, s)

	_, err := s.BatchUpsertImages(ctx, sid, []protocol.ImageInput{{Filename: "a.NEF"}})
	if err == nil {
		t.Fatal("chấp nhận clientId rỗng — mất luôn tính idempotent")
	}
}

// testListSessionsIsPerUser: danh sách buổi chụp là dữ liệu riêng tư.
//
// Lọc theo người dùng nằm ở tầng store, nên nó phải được kiểm ở đây chứ không
// chỉ ở tầng HTTP: quên một lần lọc là lộ toàn bộ buổi chụp của người khác, và
// đó là kiểu lỗi không ai phát hiện ra cho tới khi có người thấy tên khách hàng
// của người lạ trong app của mình.
func testListSessionsIsPerUser(t *testing.T, s store.Store) {
	ctx := context.Background()
	alice := newUserID(t, s)
	bob := newUserID(t, s)
	base := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)

	older, err := s.CreateSession(ctx, alice, "Buổi cũ", "Khách A", base)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	newer, err := s.CreateSession(ctx, alice, "Buổi mới", "Khách B", base.Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.CreateSession(ctx, bob, "Của người khác", "", base.Add(2*time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	res, err := s.BatchUpsertImages(ctx, older.ID, mkImages(1, 3))
	if err != nil {
		t.Fatalf("BatchUpsertImages: %v", err)
	}
	if err := s.SoftDeleteImage(ctx, res.IDs["DSC_100000"]); err != nil {
		t.Fatalf("SoftDeleteImage: %v", err)
	}

	list, err := s.ListSessions(ctx, alice, 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	if len(list) != 2 {
		t.Fatalf("nhận %d buổi chụp, mong đợi 2 — buổi của người khác KHÔNG được lọt vào", len(list))
	}
	// Buổi mới nhất trước: trong lúc chụp, buổi đang diễn ra là thứ người dùng
	// mở ra, không phải buổi tháng trước.
	if list[0].ID != newer.ID || list[1].ID != older.ID {
		t.Fatalf("sai thứ tự: nhận %q rồi %q, mong đợi mới nhất trước",
			list[0].Name, list[1].Name)
	}
	if list[0].ImageCount != 0 {
		t.Errorf("buổi chưa có ảnh đếm ra %d", list[0].ImageCount)
	}
	// 3 ảnh, xoá mềm 1: ảnh đã xoá không được tính.
	if list[1].ImageCount != 2 {
		t.Errorf("đếm ra %d ảnh, mong đợi 2 (ảnh xoá mềm không được tính)", list[1].ImageCount)
	}
}
