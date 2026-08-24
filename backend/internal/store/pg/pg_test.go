package pg_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hauph/camera/backend/internal/ids"
	"github.com/hauph/camera/backend/internal/migrate"
	"github.com/hauph/camera/backend/internal/store"
	"github.com/hauph/camera/backend/internal/store/pg"
	"github.com/hauph/camera/backend/internal/store/storetest"
)

// Bản pg chạy qua ĐÚNG bộ test tuân thủ mà bản in-memory chạy.
//
// Đó là toàn bộ mục đích của storetest: bản in-memory được dùng trong unit test
// của tầng trên, bản pg chạy production. Nếu chúng hành xử khác nhau thì mọi test
// đều xanh trong khi production sai — và không có tín hiệu nào cho tới khi khách
// hàng báo.

const defaultDSN = "postgres://camera:camera@127.0.0.1:5433/camera?sslmode=disable"

func dsn() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return defaultDSN
}

// pgStore bọc pg.Store để cung cấp user thật cho bộ test tuân thủ.
//
// Postgres có ràng buộc sessions.user_id REFERENCES users(id), nên không thể
// dùng chuỗi "user-1" tuỳ tiện như bản in-memory. Đây chính là loại khác biệt mà
// bộ test dùng chung tồn tại để phơi bày.
type pgStore struct {
	*pg.Store
	pool *pgxpool.Pool
}

func (p *pgStore) TestUserID(t *testing.T) string {
	t.Helper()
	id := ids.New()
	_, err := p.pool.Exec(context.Background(),
		"INSERT INTO users (id, email) VALUES ($1, $2)", id, id+"@example.test")
	if err != nil {
		t.Fatalf("tạo user: %v", err)
	}
	return id
}

func newStore(t *testing.T) store.Store {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(dsn())
	if err != nil {
		t.Fatalf("phân tích DSN: %v", err)
	}
	// Mỗi test một schema riêng: tạo/xoá tức thì và các test song song không
	// giẫm lên nhau.
	schema := fmt.Sprintf("t%d", time.Now().UnixNano())
	cfg.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Skipf("không kết nối được Postgres — bỏ qua (%v)", err)
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
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = pool.Exec(c, "DROP SCHEMA "+schema+" CASCADE")
		pool.Close()
	})

	if err := migrate.Run(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &pgStore{Store: pg.NewStore(pool, time.Now), pool: pool}
}

func TestConformance(t *testing.T) {
	storetest.Run(t, newStore)
}
