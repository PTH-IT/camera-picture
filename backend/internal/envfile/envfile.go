// Package envfile nạp biến môi trường từ file `.env` khi phát triển.
//
// Tự viết thay vì thêm dependency: việc cần làm là đọc `KEY=VALUE` theo dòng,
// và đường khởi động là chỗ mỗi dependency đều đáng cân nhắc kỹ.
package envfile

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Load nạp file .env nếu có.
//
// KHÔNG ghi đè biến môi trường đã được đặt sẵn. Đây là điểm quan trọng nhất:
// trên production và CI, cấu hình đến từ môi trường thật, và một file .env bị
// lỡ tay commit hoặc còn sót trong image không được phép đè lên nó. File chỉ
// điền vào chỗ trống.
//
// Không tìm thấy file là chuyện bình thường, không phải lỗi — production không
// có .env.
func Load(paths ...string) error {
	if len(paths) == 0 {
		// Thử cả thư mục hiện tại lẫn thư mục cha: `go run ./cmd/api` chạy từ
		// backend/, còn .env thường nằm ở gốc repo cạnh docker-compose.yml.
		paths = []string{".env", filepath.Join("..", ".env")}
	}

	for _, p := range paths {
		if err := loadFile(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}
		return nil
	}
	return nil
}

func loadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		// Đã có trong môi trường thì giữ nguyên. Xem chú thích của Load.
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		if err := os.Setenv(key, unquote(strings.TrimSpace(value))); err != nil {
			return err
		}
	}
	return sc.Err()
}

// unquote bỏ dấu nháy bao ngoài nếu có.
//
// Giá trị như đường dẫn có khoảng trắng hoặc khoá base64 kết thúc bằng `=` cần
// được bọc nháy trong file; giữ nguyên dấu nháy sẽ khiến chúng thành một phần
// của giá trị và lỗi rất khó thấy — ví dụ khoá mã hoá dài thêm hai ký tự.
func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}
