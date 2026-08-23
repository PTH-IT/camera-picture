package memory

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hauph/camera/backend/internal/protocol"
	"github.com/hauph/camera/backend/internal/store"
)

func newTestStore() *Store {
	n := 0
	base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	tick := 0
	return New(
		func() string { n++; return fmt.Sprintf("id-%03d", n) },
		func() time.Time { tick++; return base.Add(time.Duration(tick) * time.Second) },
	)
}

func mkSession(t *testing.T, s *Store) string {
	t.Helper()
	sess, err := s.CreateSession(context.Background(), "user-1", "Đám cưới", "Khách A", time.Now())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return sess.ID
}

func mkImages(sessionSeq, n int) []protocol.ImageInput {
	out := make([]protocol.ImageInput, n)
	base := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	for i := range out {
		out[i] = protocol.ImageInput{
			ClientID:   fmt.Sprintf("DSC_%04d", sessionSeq*10000+i),
			Filename:   fmt.Sprintf("DSC_%04d.NEF", i),
			Format:     protocol.FormatNEF,
			ByteSize:   55 * 1024 * 1024,
			CapturedAt: base.Add(time.Duration(i) * time.Second),
			IsRaw:      true,
		}
	}
	return out
}

// TestBatchUpsertIsIdempotent là điều kiện sống còn: buổi chụp thật hay rớt mạng
// và client buộc phải gửi lại lô đã gửi mà không biết lần trước có tới nơi không.
func TestBatchUpsertIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
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
	// client khác sẽ phải đồng bộ lại vô ích, tạo một vòng lặp tự nuôi tốn pin
	// và băng thông mà không ai nhận ra.
	if second.Revision != first.Revision {
		t.Errorf("revision nhảy từ %d lên %d khi retry mù — sẽ gây đồng bộ lặp vô ích",
			first.Revision, second.Revision)
	}

	// Nhưng thay đổi THẬT thì phải được ghi nhận.
	in[3].Filename = "DSC_0003_edited.NEF"
	third, err := s.BatchUpsertImages(ctx, sid, in)
	if err != nil {
		t.Fatalf("lần 3: %v", err)
	}
	if third.Updated != 1 {
		t.Errorf("updated = %d, muốn 1 — thay đổi thật phải được ghi nhận", third.Updated)
	}
	if third.Revision <= second.Revision {
		t.Errorf("revision không tăng sau thay đổi thật: %d -> %d", second.Revision, third.Revision)
	}
}

// TestDeltaSyncLosesNothing là test quan trọng nhất của package.
//
// Nó mô phỏng một client đồng bộ qua NHIỀU TRANG và khẳng định mọi bản ghi đến
// đúng một lần. Đây là chỗ ẩn náu của lỗi tốn kém nhất: nếu nhiều bản ghi dùng
// chung một revision, client sẽ lấy nửa nhóm, đặt con trỏ bằng revision đó, và
// nửa còn lại vĩnh viễn không thoả "> since". Ảnh biến mất mà không có lỗi nào.
func TestDeltaSyncLosesNothing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
	sid := mkSession(t, s)

	const total = 437
	if _, err := s.BatchUpsertImages(ctx, sid, mkImages(0, total)); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Xen kẽ chỉnh sửa để ảnh và edit trộn lẫn trong dòng revision.
	for i := 0; i < 60; i++ {
		id := fmt.Sprintf("id-%03d", i+2) // id-001 là session
		if _, err := s.PutEdit(ctx, id, protocol.PutEditRequest{Rating: 4, DeviceID: "iphone"}); err != nil {
			t.Fatalf("PutEdit %s: %v", id, err)
		}
	}

	// Đồng bộ theo trang nhỏ để chắc chắn có nhiều vòng.
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
	if len(seenEdits) != 60 {
		t.Errorf("thấy %d edit, muốn 60", len(seenEdits))
	}
	t.Logf("hội tụ sau %d vòng, con trỏ cuối = %d", rounds, cursor)
}

// TestChangesIsIncremental khẳng định lần đồng bộ thứ hai không kéo lại mọi thứ.
// Với một buổi chụp 2000 ảnh, kéo lại toàn bộ mỗi lần mở app là không chấp nhận được.
func TestChangesIsIncremental(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
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

	// Thêm một ảnh: chỉ ảnh đó được trả về.
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

// TestSoftDeletePropagates: xoá phải đến được client offline.
func TestSoftDeletePropagates(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
	sid := mkSession(t, s)

	res, err := s.BatchUpsertImages(ctx, sid, mkImages(0, 3))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	synced, _ := s.Changes(ctx, sid, 0, 500)

	target := res.IDs["DSC_0001"]
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
}

// TestConfirmAssetBumpsRevision: khi ảnh đã có bản gốc trên server, các client
// khác phải biết — nếu không, thiết bị thứ hai sẽ tưởng ảnh vẫn chỉ nằm trên thẻ.
func TestConfirmAssetBumpsRevision(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
	sid := mkSession(t, s)

	res, _ := s.BatchUpsertImages(ctx, sid, mkImages(0, 2))
	synced, _ := s.Changes(ctx, sid, 0, 500)

	target := res.IDs["DSC_0000"]
	err := s.ConfirmAsset(ctx, target, protocol.ConfirmAssetRequest{
		Tier:       protocol.TierOriginal,
		StorageKey: "s3://bucket/orig/DSC_0000.NEF",
		ByteSize:   55 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("ConfirmAsset: %v", err)
	}

	after, _ := s.Changes(ctx, sid, synced.Revision, 500)
	if len(after.Images) != 1 {
		t.Fatalf("trả về %d ảnh, muốn 1", len(after.Images))
	}
	if _, ok := after.Images[0].Assets[protocol.TierOriginal]; !ok {
		t.Errorf("asset original không có trong bản đồng bộ: %+v", after.Images[0].Assets)
	}
}

// TestImageWithoutAssetsIsNormal ghi lại một bất biến của kiến trúc: phần lớn
// ảnh KHÔNG BAO GIỜ lên server. Nếu ai đó thêm ràng buộc "ảnh phải có asset",
// test này vỡ và nhắc họ đọc lại ADR 0001.
func TestImageWithoutAssetsIsNormal(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
	sid := mkSession(t, s)

	res, err := s.BatchUpsertImages(ctx, sid, mkImages(0, 1))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	img, err := s.GetImage(ctx, res.IDs["DSC_0000"])
	if err != nil {
		t.Fatalf("GetImage: %v", err)
	}
	if len(img.Assets) != 0 {
		t.Errorf("ảnh mới phải không có asset nào, có %d", len(img.Assets))
	}
}

func TestUnknownSession(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()

	if _, err := s.Changes(ctx, "không-tồn-tại", 0, 10); err != store.ErrNotFound {
		t.Errorf("Changes trả %v, muốn ErrNotFound", err)
	}
	if _, err := s.BatchUpsertImages(ctx, "không-tồn-tại", mkImages(0, 1)); err != store.ErrNotFound {
		t.Errorf("BatchUpsertImages trả %v, muốn ErrNotFound", err)
	}
}

// TestConcurrentAccessIsRaceFree tồn tại để chạy dưới `go test -race` trong CI.
//
// Nó bắt một lỗi cụ thể và dễ tái phát: trả bản ghi ra ngoài bằng copy NÔNG.
// `rec := row.rec` sao chép struct nhưng Assets là map, nên bản sao vẫn dùng
// chung map với store — người gọi đọc map đó trong khi goroutine khác chạy
// ConfirmAsset là tranh chấp dữ liệu thật. Mutex không bảo vệ được gì sau khi
// giá trị đã rời khỏi hàm.
//
// Không có -race thì test này luôn xanh, kể cả khi lỗi đã quay lại. Đó chính là
// lý do CI chạy với -race.
func TestConcurrentAccessIsRaceFree(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()
	sid := mkSession(t, s)

	res, err := s.BatchUpsertImages(ctx, sid, mkImages(0, 20))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	ids := make([]string, 0, len(res.IDs))
	for _, id := range res.IDs {
		ids = append(ids, id)
	}

	var wg sync.WaitGroup
	const rounds = 40

	// Ghi: liên tục gắn asset và chỉnh sửa.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			id := ids[i%len(ids)]
			_ = s.ConfirmAsset(ctx, id, protocol.ConfirmAssetRequest{
				Tier:       protocol.TierPreview,
				StorageKey: fmt.Sprintf("s3://b/%d", i),
				ByteSize:   int64(i),
			})
			_, _ = s.PutEdit(ctx, id, protocol.PutEditRequest{
				Rating:    i % 6,
				Overrides: map[string]any{"exposure": i},
			})
		}
	}()

	// Đọc: đồng bộ delta và ĐỌC SÂU vào các map trả về. Chỉ đọc struct ngoài
	// thì không lộ lỗi — phải chạm vào map mới kích hoạt race detector.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				resp, err := s.Changes(ctx, sid, 0, 500)
				if err != nil {
					continue
				}
				for _, img := range resp.Images {
					for tier, a := range img.Assets {
						_, _ = tier, a.ByteSize
					}
				}
				for _, ed := range resp.Edits {
					for k, v := range ed.Overrides {
						_, _ = k, v
					}
				}
				if img, err := s.GetImage(ctx, ids[i%len(ids)]); err == nil {
					for range img.Assets {
					}
				}
			}
		}()
	}

	wg.Wait()
}
