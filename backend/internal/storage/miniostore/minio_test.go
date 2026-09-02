package miniostore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hauph/camera/backend/internal/storage"
)

// Bộ test này chạy với MinIO THẬT, không mock.
//
// Lý do: thứ dễ sai nhất ở tầng này là hành vi của presigned URL — chữ ký ràng
// buộc những gì, không ràng buộc những gì. Mock sẽ chỉ khẳng định lại giả định
// của chính mình, và giả định đó chính là chỗ có lỗi.
//
// Chạy MinIO cho test:
//
//	docker run -d --name camera-minio-test -p 9100:9000 \
//	  -e MINIO_ROOT_USER=testkey -e MINIO_ROOT_PASSWORD=testsecret123 \
//	  minio/minio:latest server /data
//
// Không có MinIO thì test tự bỏ qua thay vì làm đỏ CI.

const (
	testEndpoint = "127.0.0.1:9100"
	testAccess   = "testkey"
	testSecret   = "testsecret123"
)

type memUsage struct {
	mu sync.Mutex
	m  map[string]int64
}

func newMemUsage() *memUsage { return &memUsage{m: map[string]int64{}} }

func (u *memUsage) Used(_ context.Context, userID string) (int64, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.m[userID], nil
}

func (u *memUsage) Add(_ context.Context, userID string, delta int64) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.m[userID] += delta
	if u.m[userID] < 0 {
		u.m[userID] = 0
	}
	return nil
}

func newTestStore(t *testing.T, quotaBytes int64) (*Store, *memUsage) {
	t.Helper()

	if os.Getenv("SKIP_MINIO_TESTS") != "" {
		t.Skip("SKIP_MINIO_TESTS được đặt")
	}
	// Kiểm tra nhanh trước khi dựng client, để thông báo bỏ qua rõ ràng thay vì
	// một lỗi timeout khó hiểu.
	c := &http.Client{Timeout: 2 * time.Second}
	if _, err := c.Get("http://" + testEndpoint + "/minio/health/live"); err != nil {
		t.Skipf("không có MinIO tại %s — bỏ qua test tích hợp (%v)", testEndpoint, err)
	}

	// Cho phép trỏ vào MinIO đang có sẵn trên máy (ví dụ container của
	// docker-compose, vốn dùng khoá khác). Không có biến môi trường thì giữ khoá
	// mặc định trong chú thích đầu file.
	access := envOr("MINIO_TEST_ACCESS_KEY", testAccess)
	secret := envOr("MINIO_TEST_SECRET_KEY", testSecret)

	usage := newMemUsage()
	bucket := fmt.Sprintf("test-%d", time.Now().UnixNano())
	s, err := New(Config{
		Endpoint: testEndpoint, AccessKey: access, SecretKey: secret,
		Bucket: bucket, UseSSL: false,
	}, func(context.Context, string) (int64, error) { return quotaBytes, nil }, usage)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.EnsureBucket(context.Background()); err != nil {
		// Có MinIO ở cổng đó nhưng KHÔNG nhận khoá này — thường là container của
		// docker-compose (devkey) chứ không phải container test. Bỏ qua kèm chỉ
		// dẫn, thay vì báo đỏ một thứ mà người chạy không hề làm sai.
		t.Skipf("MinIO tại %s không nhận khoá test (%v).\n"+
			"Chạy container test theo chú thích đầu file, hoặc đặt "+
			"MINIO_TEST_ACCESS_KEY/MINIO_TEST_SECRET_KEY cho instance đang có.",
			testEndpoint, err)
	}
	return s, usage
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// putTo tải dữ liệu lên bằng chính URL presigned, đúng như client thật làm.
func putTo(t *testing.T, url string, data []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("tạo request: %v", err)
	}
	req.ContentLength = int64(len(data))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	return resp
}

func TestUploadDownloadRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, usage := newTestStore(t, 10<<20)

	data := bytes.Repeat([]byte("NEF"), 1000) // 3000 byte
	target, err := s.Upload(ctx, "u1", "sessions/s1/DSC_0001.NEF", int64(len(data)))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if target.Provider != storage.ProviderManaged {
		t.Errorf("Provider = %q", target.Provider)
	}

	resp := putTo(t, target.URL, data)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT trả %d", resp.StatusCode)
	}

	size, err := s.Confirm(ctx, "u1", target.Key)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if size != int64(len(data)) {
		t.Errorf("kích thước = %d, muốn %d", size, len(data))
	}
	if used, _ := usage.Used(ctx, "u1"); used != int64(len(data)) {
		t.Errorf("đã dùng = %d, muốn %d", used, len(data))
	}

	// Tải về bằng URL presigned và so sánh từng byte.
	dl, err := s.Download(ctx, "u1", target.Key)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := http.Get(dl.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer got.Body.Close()
	body, _ := io.ReadAll(got.Body)
	if !bytes.Equal(body, data) {
		t.Errorf("dữ liệu tải về khác dữ liệu đã tải lên (%d vs %d byte)", len(body), len(data))
	}
}

// TestClientCannotLieAboutSize là test quan trọng nhất của package.
//
// Presigned PUT KHÔNG ràng buộc kích thước — chữ ký chỉ ràng buộc bucket, khoá,
// phương thức và thời hạn. Client khai 100 byte rồi tải lên 5MB thì S3 vẫn nhận
// bình thường.
//
// Nếu tin vào kích thước client khai, hạn mức trở thành vô nghĩa: ai cũng khai
// 1 byte rồi tải lên bao nhiêu tuỳ thích. Confirm là nơi cưỡng chế thật, bằng
// cách hỏi kích thước THẬT từ storage.
func TestClientCannotLieAboutSize(t *testing.T) {
	ctx := context.Background()
	const quota = 1 << 20 // 1MB
	s, usage := newTestStore(t, quota)

	// Khai 100 byte — dưới hạn mức, nên được cấp URL.
	target, err := s.Upload(ctx, "u1", "gian-lan.bin", 100)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Thực tế tải lên 2MB, gấp đôi hạn mức.
	big := bytes.Repeat([]byte("x"), 2<<20)
	resp := putTo(t, target.URL, big)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT trả %d — MinIO đã chặn, giả định của test sai", resp.StatusCode)
	}
	t.Log("xác nhận: presigned PUT KHÔNG chặn được kích thước, S3 nhận 2MB dù khai 100 byte")

	// Confirm phải phát hiện và từ chối.
	_, err = s.Confirm(ctx, "u1", target.Key)
	if !errors.Is(err, storage.ErrQuotaExceeded) {
		t.Fatalf("Confirm chấp nhận file vượt hạn mức — lỗi = %v", err)
	}

	// Và đối tượng phải bị xoá, không để lại rác chiếm chỗ.
	if dl, err := s.Download(ctx, "u1", target.Key); err == nil {
		r, err := http.Get(dl.URL)
		if err == nil {
			defer r.Body.Close()
			if r.StatusCode == http.StatusOK {
				t.Error("đối tượng vượt hạn mức vẫn còn trên storage")
			}
		}
	}
	if used, _ := usage.Used(ctx, "u1"); used != 0 {
		t.Errorf("đã dùng = %d, muốn 0 — file bị từ chối không được tính vào hạn mức", used)
	}
}

func TestQuotaBlocksDeclaredOversize(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, 1<<20)

	_, err := s.Upload(ctx, "u1", "qua-lon.NEF", 2<<20)
	if !errors.Is(err, storage.ErrQuotaExceeded) {
		t.Errorf("lỗi = %v, muốn ErrQuotaExceeded", err)
	}
}

// TestPathTraversalRejected: presigned URL không kiểm tra gì ngoài chữ ký, nên
// nếu khoá do client gửi được dùng nguyên văn, "../nguoi-khac/anh.NEF" sẽ đọc
// được file của người khác.
func TestPathTraversalRejected(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, 10<<20)

	bad := []string{
		"../nguoi-khac/anh.NEF",
		"a/../../nguoi-khac/anh.NEF",
		"..",
		"",
		"   ",
	}
	for _, k := range bad {
		t.Run(k, func(t *testing.T) {
			if _, err := s.Upload(ctx, "u1", k, 100); !errors.Is(err, ErrUnsafeKey) {
				t.Errorf("khoá %q được chấp nhận — lỗi = %v", k, err)
			}
		})
	}
}

// TestKeysAreNamespacedPerUser: khoá của hai người dùng khác nhau không bao giờ
// trỏ tới cùng một đối tượng, kể cả khi họ gửi lên cùng một tên file.
func TestKeysAreNamespacedPerUser(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, 10<<20)

	a, err := s.Upload(ctx, "nguoi-a", "DSC_0001.NEF", 100)
	if err != nil {
		t.Fatalf("Upload a: %v", err)
	}
	b, err := s.Upload(ctx, "nguoi-b", "DSC_0001.NEF", 100)
	if err != nil {
		t.Fatalf("Upload b: %v", err)
	}

	if a.Key == b.Key {
		t.Fatalf("hai người dùng cùng khoá đối tượng: %q", a.Key)
	}
	if !strings.HasPrefix(a.Key, "users/nguoi-a/") || !strings.HasPrefix(b.Key, "users/nguoi-b/") {
		t.Errorf("khoá không nằm dưới tiền tố người dùng: %q, %q", a.Key, b.Key)
	}
}

// TestConfirmRejectsForeignKey: kẻ tấn công gọi Confirm với khoá của người khác
// để cộng dung lượng của nạn nhân vào hạn mức của mình, hoặc để dò sự tồn tại.
func TestConfirmRejectsForeignKey(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, 10<<20)

	target, err := s.Upload(ctx, "nan-nhan", "rieng-tu.NEF", 100)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	putTo(t, target.URL, []byte("du lieu rieng tu")).Body.Close()

	if _, err := s.Confirm(ctx, "ke-tan-cong", target.Key); !errors.Is(err, ErrUnsafeKey) {
		t.Errorf("Confirm chấp nhận khoá của người khác — lỗi = %v", err)
	}
}

// TestDeleteFreesQuota: xoá trước rồi mới hỏi kích thước thì không bao giờ biết
// đã giải phóng bao nhiêu, và hạn mức sẽ trôi dần cho tới khi người dùng không
// upload được gì dù đã xoá hết ảnh.
func TestDeleteFreesQuota(t *testing.T) {
	ctx := context.Background()
	s, usage := newTestStore(t, 10<<20)

	data := bytes.Repeat([]byte("A"), 4096)
	target, err := s.Upload(ctx, "u1", "tam.NEF", int64(len(data)))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	putTo(t, target.URL, data).Body.Close()
	if _, err := s.Confirm(ctx, "u1", target.Key); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if used, _ := usage.Used(ctx, "u1"); used != int64(len(data)) {
		t.Fatalf("đã dùng = %d, muốn %d", used, len(data))
	}

	if err := s.Delete(ctx, "u1", target.Key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if used, _ := usage.Used(ctx, "u1"); used != 0 {
		t.Errorf("sau khi xoá đã dùng = %d, muốn 0 — hạn mức bị rò rỉ", used)
	}
}

func TestCapabilities(t *testing.T) {
	s, _ := newTestStore(t, 1<<20)

	for _, c := range []storage.Capability{
		storage.CapServerSideRender, storage.CapEnforcedQuota, storage.CapDurable,
	} {
		if !storage.Has(s, c) {
			t.Errorf("thiếu khả năng %q", c)
		}
	}
}
