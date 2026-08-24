package migrate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Test chạy với Postgres THẬT. Không có Postgres thì tự bỏ qua.
//
//	docker compose up -d postgres
//
// Lý do không mock: migration là DDL, và cái cần kiểm tra chính là Postgres có
// chấp nhận SQL đó hay không. Mock sẽ luôn nói có.

// DSN mặc định khớp docker-compose.yml (cổng 5433 để không đụng Postgres khác).
const defaultDSN = "postgres://camera:camera@127.0.0.1:5433/camera?sslmode=disable"

func dsn() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return defaultDSN
}

// newTestDB dựng một SCHEMA riêng cho mỗi test.
//
// Dùng schema riêng thay vì database riêng: tạo database tốn thời gian và cần
// kết nối tới database khác. Schema thì tạo/xoá tức thì, và search_path khiến mọi
// câu lệnh không đủ điều kiện chạy đúng vào đó — nên các test chạy song song
// không giẫm lên nhau.
func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(dsn())
	if err != nil {
		t.Fatalf("phân tích DSN: %v", err)
	}

	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	cfg.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Skipf("không kết nối được Postgres — bỏ qua test tích hợp (%v)", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("không có Postgres tại %s — bỏ qua test tích hợp (%v)", dsn(), err)
	}

	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		pool.Close()
		t.Fatalf("tạo schema: %v", err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(c, "DROP SCHEMA "+schema+" CASCADE")
		pool.Close()
	})
	return pool
}

// TestMigrationsApply là test có giá trị nhất ở đây: nó chứng minh SQL trong
// migrations/ thật sự chạy được trên Postgres. Viết SQL không ai chạy thử là cách
// chắc chắn để phát hiện lỗi cú pháp lúc triển khai production.
func TestMigrationsApply(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	if err := Run(ctx, pool); err != nil {
		t.Fatalf("Run: %v", err)
	}

	applied, err := Applied(ctx, pool)
	if err != nil {
		t.Fatalf("Applied: %v", err)
	}
	if len(applied) < 2 {
		t.Fatalf("áp dụng %d migration, muốn ít nhất 2: %v", len(applied), applied)
	}
	t.Logf("đã áp dụng: %v", applied)

	// Kiểm tra vài bảng và cột mà tầng trên phụ thuộc vào.
	wantTables := []string{
		"users", "identities", "user_passwords", "sessions_auth",
		"sessions", "images", "image_assets", "presets", "image_edits",
		"storage_links", "entitlements", "storage_usage",
	}
	for _, tbl := range wantTables {
		var n int
		err := pool.QueryRow(ctx, `
			SELECT count(*) FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = $1`, tbl).Scan(&n)
		if err != nil {
			t.Fatalf("truy vấn bảng %s: %v", tbl, err)
		}
		if n != 1 {
			t.Errorf("thiếu bảng %q", tbl)
		}
	}
}

// TestEmailIsNullable canh giữ một sửa lỗi cụ thể trong migration 0002.
//
// Migration 0001 khai users.email NOT NULL. Điều đó sai với thực tế: người dùng
// Sign in with Apple được phép ẩn email HOÀN TOÀN, và khi đó ta không có địa chỉ
// nào để lưu. Ép NOT NULL là từ chối chính nhóm người dùng mà Apple bắt ta phải
// hỗ trợ. Nếu ai đó "dọn dẹp" migration và khôi phục NOT NULL, test này vỡ.
func TestEmailIsNullable(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)
	if err := Run(ctx, pool); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var nullable string
	err := pool.QueryRow(ctx, `
		SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'users' AND column_name = 'email'`).
		Scan(&nullable)
	if err != nil {
		t.Fatalf("truy vấn cột: %v", err)
	}
	if nullable != "YES" {
		t.Error("users.email đang NOT NULL — người dùng Apple ẩn email sẽ không đăng ký được")
	}

	// Và phải chèn được nhiều dòng email NULL: hai người ẩn email không được
	// coi là trùng nhau.
	for i := 0; i < 3; i++ {
		_, err := pool.Exec(ctx, "INSERT INTO users (id, email) VALUES (gen_random_uuid(), NULL)")
		if err != nil {
			t.Fatalf("chèn user thứ %d không có email: %v", i+1, err)
		}
	}
}

func TestRunIsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	for i := 0; i < 3; i++ {
		if err := Run(ctx, pool); err != nil {
			t.Fatalf("lần %d: %v", i+1, err)
		}
	}
	applied, _ := Applied(ctx, pool)

	seen := map[string]bool{}
	for _, v := range applied {
		if seen[v] {
			t.Errorf("phiên bản %q được ghi nhiều lần", v)
		}
		seen[v] = true
	}
}

// TestConcurrentRunIsSafe mô phỏng rolling deploy: nhiều bản sao khởi động cùng
// lúc và cùng chạy migration. Không có advisory lock, hai bản có thể cùng thấy
// migration chưa áp dụng, cùng chạy nó, và một trong hai lỗi với DDL đã thực thi
// một phần.
func TestConcurrentRunIsSafe(t *testing.T) {
	ctx := context.Background()
	pool := newTestDB(t)

	const n = 5
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = Run(ctx, pool)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("bản sao %d lỗi: %v", i, err)
		}
	}

	applied, _ := Applied(ctx, pool)
	if len(applied) < 2 {
		t.Errorf("áp dụng %d migration sau khi chạy song song, muốn ít nhất 2", len(applied))
	}
}

// TestFilenamesSortCorrectly: thứ tự tên file LÀ thứ tự áp dụng, nên tiền tố số
// phải giữ cho thứ tự từ điển trùng thứ tự số. "10_x.sql" đứng trước "9_x.sql"
// theo từ điển — đó là lý do dùng bốn chữ số.
func TestFilenamesSortCorrectly(t *testing.T) {
	names, err := list()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, n := range names {
		if len(n) < 5 || !strings.HasPrefix(n, "0") {
			t.Errorf("migration %q không có tiền tố số bốn chữ số — thứ tự áp dụng sẽ sai khi tới file thứ 10", n)
		}
	}
}
