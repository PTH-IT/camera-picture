// Package secrets mã hoá dữ liệu nhạy cảm trước khi ghi xuống cơ sở dữ liệu.
//
// Hiện dùng cho refresh token của Google Drive. Refresh token là thứ cho phép
// đọc và ghi vào Drive của người dùng gần như vô thời hạn — nếu cơ sở dữ liệu bị
// lộ mà token nằm ở dạng thô, kẻ tấn công truy cập được Drive của TẤT CẢ người
// dùng đã liên kết, và điều đó không thu hồi được bằng cách đổi mật khẩu.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
)

var (
	ErrKeyLength    = errors.New("khoá phải đúng 32 byte")
	ErrCiphertext   = errors.New("bản mã không hợp lệ")
	ErrWrongContext = errors.New("bản mã không thuộc ngữ cảnh này")
)

// Cipher mã hoá AES-256-GCM.
type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("%w, nhận được %d", ErrKeyLength, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// FromEnv đọc khoá base64 từ biến môi trường.
//
// Không có khoá mặc định và không tự sinh khoá tạm: một khoá "tạm" sinh lúc khởi
// động nghĩa là mọi refresh token đã lưu trở thành rác sau lần restart đầu tiên,
// và người dùng phải liên kết lại Drive mà không hiểu vì sao.
func FromEnv(name string) (*Cipher, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return nil, fmt.Errorf("thiếu biến môi trường %s", name)
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%s không phải base64 hợp lệ: %w", name, err)
	}
	return NewCipher(key)
}

// GenerateKey sinh khoá mới dạng base64, để đưa vào biến môi trường.
func GenerateKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// Encrypt mã hoá và gắn dữ liệu vào một NGỮ CẢNH.
//
// context được dùng làm additional authenticated data. Nó không được mã hoá,
// nhưng bản mã chỉ giải được khi truyền đúng context.
//
// Vì sao cần: không có ràng buộc ngữ cảnh, một người có quyền ghi vào cơ sở dữ
// liệu có thể COPY bản mã refresh token của nạn nhân sang dòng của chính họ. Họ
// không đọc được token, nhưng server sẽ vui vẻ giải mã và dùng nó để truy cập
// Drive của nạn nhân thay mặt kẻ tấn công. Ràng buộc context (userID + provider)
// khiến bản mã bị chép sang dòng khác trở thành vô dụng.
func (c *Cipher) Encrypt(plaintext []byte, context string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("sinh nonce: %w", err)
	}
	// Kết quả: nonce || ciphertext||tag. Nonce ngẫu nhiên cho mỗi lần mã hoá —
	// dùng lại nonce với GCM là mất hoàn toàn tính bảo mật.
	return c.aead.Seal(nonce, nonce, plaintext, []byte(context)), nil
}

func (c *Cipher) Decrypt(data []byte, context string) ([]byte, error) {
	ns := c.aead.NonceSize()
	if len(data) < ns {
		return nil, ErrCiphertext
	}
	out, err := c.aead.Open(nil, data[:ns], data[ns:], []byte(context))
	if err != nil {
		// GCM không phân biệt được "sai khoá", "sai context" và "bị sửa" — cả ba
		// đều là xác thực thất bại. Trả lỗi chung là đúng: nói rõ hơn cũng chỉ
		// giúp người đang dò.
		return nil, ErrWrongContext
	}
	return out, nil
}

// LinkContext dựng chuỗi ngữ cảnh cho refresh token của một liên kết lưu trữ.
//
// Phải khớp chính xác giữa lúc ghi và lúc đọc, nên tập trung ở một hàm thay vì
// nối chuỗi rải rác.
func LinkContext(userID, provider string) string {
	return "storage_link\x00" + userID + "\x00" + provider
}
