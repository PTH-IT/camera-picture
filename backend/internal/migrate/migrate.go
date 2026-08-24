// Package migrate áp dụng các migration SQL đã nhúng.
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hauph/camera/backend/migrations"
)

// advisoryLockKey là khoá cho pg_advisory_lock.
//
// Cần khoá vì khi triển khai nhiều bản sao (rolling deploy, Kubernetes), tất cả
// đều khởi động cùng lúc và cùng chạy migration. Không khoá thì hai bản sao có
// thể cùng thấy migration chưa áp dụng, cùng chạy nó, và một trong hai sẽ lỗi
// giữa chừng — với DDL đã thực thi một phần.
//
// Số cụ thể không quan trọng, chỉ cần cố định và không đụng hệ thống khác.
const advisoryLockKey = 8127346501

// Run áp dụng mọi migration chưa chạy, theo thứ tự tên file.
func Run(ctx context.Context, pool *pgxpool.Pool) error {
	files, err := list()
	if err != nil {
		return err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("lấy kết nối: %w", err)
	}
	defer conn.Release()

	// Giữ khoá trên MỘT kết nối suốt quá trình. Lấy khoá từ pool rồi trả về
	// giữa chừng sẽ tự động nhả khoá và mất tác dụng.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		return fmt.Errorf("lấy khoá migration: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", advisoryLockKey)
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("tạo bảng schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, conn.Conn())
	if err != nil {
		return err
	}

	for _, name := range files {
		version := strings.TrimSuffix(name, ".sql")
		if applied[version] {
			continue
		}

		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("đọc %s: %w", name, err)
		}

		// Mỗi migration chạy trong MỘT giao dịch. Postgres hỗ trợ DDL trong giao
		// dịch, nên một migration lỗi giữa chừng sẽ được hoàn tác trọn vẹn thay vì
		// để lại lược đồ ở trạng thái nửa vời — thứ rất khó sửa bằng tay trên
		// production.
		if err := inTx(ctx, conn.Conn(), func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return fmt.Errorf("chạy %s: %w", name, err)
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// Applied trả về danh sách migration đã chạy. Dùng cho endpoint kiểm tra sức khoẻ.
func Applied(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	m, err := appliedVersions(ctx, conn.Conn())
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out, nil
}

func appliedVersions(ctx context.Context, conn *pgx.Conn) (map[string]bool, error) {
	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("đọc schema_migrations: %w", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func list() ([]string, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	// Thứ tự tên file LÀ thứ tự áp dụng. Tiền tố số bốn chữ số giữ cho thứ tự
	// từ điển trùng với thứ tự số.
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("không tìm thấy migration nào — kiểm tra chỉ thị go:embed")
	}
	return names, nil
}

func inTx(ctx context.Context, conn *pgx.Conn, fn func(pgx.Tx) error) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return err
	}
	return tx.Commit(ctx)
}
