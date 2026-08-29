package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("ghi file: %v", err)
	}
	return p
}

func TestLoadsValues(t *testing.T) {
	p := write(t, "FOO=bar\nBAZ = qux \n")
	t.Setenv("FOO", "")
	os.Unsetenv("FOO")
	os.Unsetenv("BAZ")
	t.Cleanup(func() { os.Unsetenv("FOO"); os.Unsetenv("BAZ") })

	if err := Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("FOO"); got != "bar" {
		t.Errorf("FOO = %q, muốn bar", got)
	}
	if got := os.Getenv("BAZ"); got != "qux" {
		t.Errorf("BAZ = %q, muốn qux (phải cắt khoảng trắng)", got)
	}
}

// TestDoesNotOverrideExisting là hành vi quan trọng nhất của package.
//
// Trên production và CI, cấu hình đến từ môi trường thật. Một file .env bị lỡ
// tay commit hoặc còn sót trong Docker image mà đè lên được biến môi trường
// nghĩa là bản triển khai chạy bằng thông tin của máy lập trình viên — gồm cả
// khoá mã hoá và chuỗi kết nối cơ sở dữ liệu.
func TestDoesNotOverrideExisting(t *testing.T) {
	p := write(t, "DATABASE_URL=postgres://tu-file/db\n")
	t.Setenv("DATABASE_URL", "postgres://tu-moi-truong/db")

	if err := Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("DATABASE_URL"); got != "postgres://tu-moi-truong/db" {
		t.Errorf("file đã ĐÈ lên biến môi trường: %q", got)
	}
}

func TestMissingFileIsNotAnError(t *testing.T) {
	// Production không có .env. Coi việc thiếu file là lỗi sẽ khiến server từ
	// chối khởi động ở đúng nơi cấu hình đã đúng.
	if err := Load(filepath.Join(t.TempDir(), "khong-ton-tai")); err != nil {
		t.Errorf("thiếu file bị coi là lỗi: %v", err)
	}
}

func TestIgnoresCommentsAndBlanks(t *testing.T) {
	p := write(t, "# ghi chú\n\n   \nA=1\n# B=2\n")
	os.Unsetenv("A")
	os.Unsetenv("B")
	t.Cleanup(func() { os.Unsetenv("A") })

	if err := Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if os.Getenv("A") != "1" {
		t.Error("không nạp được dòng hợp lệ")
	}
	if _, ok := os.LookupEnv("B"); ok {
		t.Error("nạp cả dòng bị chú thích")
	}
}

// TestUnquotes: khoá base64 kết thúc bằng '=' và đường dẫn có khoảng trắng cần
// bọc nháy. Giữ lại dấu nháy sẽ làm giá trị dài thêm hai ký tự — với khoá mã hoá
// thì lỗi biểu hiện là "khoá phải đúng 32 byte", rất xa nguyên nhân thật.
func TestUnquotes(t *testing.T) {
	p := write(t, `KEY="abc123=="`+"\n"+`PATH_A='C:/co khoang trang/file.pem'`+"\n")
	os.Unsetenv("KEY")
	os.Unsetenv("PATH_A")
	t.Cleanup(func() { os.Unsetenv("KEY"); os.Unsetenv("PATH_A") })

	if err := Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("KEY"); got != "abc123==" {
		t.Errorf("KEY = %q, muốn abc123==", got)
	}
	if got := os.Getenv("PATH_A"); got != "C:/co khoang trang/file.pem" {
		t.Errorf("PATH_A = %q", got)
	}
}

// TestValueWithEqualsSign: chuỗi kết nối và khoá base64 đều chứa dấu '=' bên
// trong giá trị. Cắt ở dấu '=' ĐẦU TIÊN mới đúng.
func TestValueWithEqualsSign(t *testing.T) {
	p := write(t, "DSN=postgres://u:p@h/db?sslmode=disable\n")
	os.Unsetenv("DSN")
	t.Cleanup(func() { os.Unsetenv("DSN") })

	if err := Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("DSN"); got != "postgres://u:p@h/db?sslmode=disable" {
		t.Errorf("DSN = %q — có vẻ đã cắt ở dấu = cuối thay vì đầu", got)
	}
}
