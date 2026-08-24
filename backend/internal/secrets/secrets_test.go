package secrets

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"
)

func newCipher(t *testing.T) *Cipher {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("sinh khoá: %v", err)
	}
	c, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

func TestRoundTrip(t *testing.T) {
	c := newCipher(t)
	ctx := LinkContext("u1", "google_drive")
	token := []byte("1//0gRefreshTokenCuaNguoiDung")

	ct, err := c.Encrypt(token, ctx)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(ct, token) {
		t.Fatal("bản mã chứa nguyên văn token")
	}

	got, err := c.Decrypt(ct, ctx)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, token) {
		t.Errorf("giải mã ra %q, muốn %q", got, token)
	}
}

// TestCiphertextCannotBeMovedBetweenUsers là lý do tồn tại của tham số context.
//
// Kịch bản: kẻ tấn công có quyền ghi vào cơ sở dữ liệu (SQL injection, backup bị
// lộ, nhân viên cũ còn quyền). Hắn không đọc được refresh token của nạn nhân,
// nhưng CHÉP được bản mã sang dòng của chính mình. Không ràng buộc ngữ cảnh thì
// server sẽ giải mã bình thường và dùng token đó để truy cập Drive của nạn nhân
// thay mặt kẻ tấn công.
func TestCiphertextCannotBeMovedBetweenUsers(t *testing.T) {
	c := newCipher(t)
	token := []byte("refresh-token-cua-nan-nhan")

	ct, err := c.Encrypt(token, LinkContext("nan-nhan", "google_drive"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Kẻ tấn công chép bản mã sang dòng của mình.
	_, err = c.Decrypt(ct, LinkContext("ke-tan-cong", "google_drive"))
	if !errors.Is(err, ErrWrongContext) {
		t.Fatalf("GIẢI MÃ ĐƯỢC bản mã của người khác — lỗi = %v", err)
	}
}

// TestCiphertextCannotBeMovedBetweenProviders: cùng một người dùng nhưng khác
// nhà cung cấp cũng phải tách biệt.
func TestCiphertextCannotBeMovedBetweenProviders(t *testing.T) {
	c := newCipher(t)

	ct, _ := c.Encrypt([]byte("token"), LinkContext("u1", "google_drive"))
	if _, err := c.Decrypt(ct, LinkContext("u1", "icloud")); !errors.Is(err, ErrWrongContext) {
		t.Errorf("giải mã được sang nhà cung cấp khác — lỗi = %v", err)
	}
}

func TestTamperedCiphertextRejected(t *testing.T) {
	c := newCipher(t)
	ctx := LinkContext("u1", "google_drive")

	ct, _ := c.Encrypt([]byte("refresh-token"), ctx)

	// Lật một bit ở giữa phần dữ liệu.
	tampered := bytes.Clone(ct)
	tampered[len(tampered)/2] ^= 0x01

	if _, err := c.Decrypt(tampered, ctx); err == nil {
		t.Fatal("CHẤP NHẬN bản mã đã bị sửa")
	}
}

func TestWrongKeyRejected(t *testing.T) {
	a, b := newCipher(t), newCipher(t)
	ctx := LinkContext("u1", "google_drive")

	ct, _ := a.Encrypt([]byte("token"), ctx)
	if _, err := b.Decrypt(ct, ctx); err == nil {
		t.Fatal("giải mã được bằng khoá khác")
	}
}

// TestNonceIsUnique: dùng lại nonce với GCM là mất hoàn toàn tính bảo mật, nên
// hai lần mã hoá cùng nội dung phải cho hai bản mã khác nhau.
func TestNonceIsUnique(t *testing.T) {
	c := newCipher(t)
	ctx := LinkContext("u1", "google_drive")
	token := []byte("cung-mot-token")

	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		ct, err := c.Encrypt(token, ctx)
		if err != nil {
			t.Fatalf("lần %d: %v", i, err)
		}
		if seen[string(ct)] {
			t.Fatal("TRÙNG bản mã — nonce đang bị dùng lại")
		}
		seen[string(ct)] = true
	}
}

func TestShortKeyRejected(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33} {
		if _, err := NewCipher(make([]byte, n)); !errors.Is(err, ErrKeyLength) {
			t.Errorf("khoá %d byte: lỗi = %v, muốn ErrKeyLength", n, err)
		}
	}
}

func TestTruncatedCiphertextRejected(t *testing.T) {
	c := newCipher(t)
	ctx := LinkContext("u1", "google_drive")
	ct, _ := c.Encrypt([]byte("token"), ctx)

	for _, n := range []int{0, 1, c.aead.NonceSize() - 1} {
		if _, err := c.Decrypt(ct[:n], ctx); err == nil {
			t.Errorf("chấp nhận bản mã cụt %d byte", n)
		}
	}
}

func TestGenerateKeyIsUsable(t *testing.T) {
	k1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	k2, _ := GenerateKey()
	if k1 == k2 {
		t.Error("hai lần sinh ra cùng một khoá")
	}

	t.Setenv("TEST_STORAGE_KEY", k1)
	if _, err := FromEnv("TEST_STORAGE_KEY"); err != nil {
		t.Errorf("khoá vừa sinh không dùng được: %v", err)
	}
}

// TestFromEnvFailsLoudly: không có khoá thì phải dừng hẳn, không được tự sinh
// khoá tạm. Khoá tạm nghĩa là mọi refresh token đã lưu thành rác sau lần restart
// đầu tiên, và người dùng phải liên kết lại Drive mà không hiểu vì sao.
func TestFromEnvFailsLoudly(t *testing.T) {
	t.Setenv("TEST_MISSING_KEY", "")
	if _, err := FromEnv("TEST_MISSING_KEY"); err == nil {
		t.Error("thiếu khoá mà không báo lỗi")
	}

	t.Setenv("TEST_BAD_KEY", "khong-phai-base64!!!")
	if _, err := FromEnv("TEST_BAD_KEY"); err == nil {
		t.Error("khoá không phải base64 mà không báo lỗi")
	}
}
