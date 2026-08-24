// Package ids sinh định danh UUID v7.
//
// Chọn v7 chứ không phải v4: v7 mang dấu thời gian ở 48 bit đầu nên các id sinh
// gần nhau nằm gần nhau trong chỉ mục B-tree. Với v4 hoàn toàn ngẫu nhiên, mỗi
// lần chèn rơi vào một trang ngẫu nhiên của chỉ mục, khiến chỉ mục phân mảnh và
// cache đệm mất tác dụng. Bảng `images` của dự án này nhận hàng nghìn dòng mỗi
// buổi chụp, nên khác biệt đó là thật.
//
// Tự viết thay vì thêm dependency: UUID v7 chỉ là 48 bit thời gian + 74 bit ngẫu
// nhiên + 6 bit phiên bản/biến thể.
package ids

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"
)

// New sinh UUID v7 dạng chuỗi chuẩn 8-4-4-4-12.
func New() string {
	return NewAt(time.Now())
}

// NewAt sinh UUID v7 với dấu thời gian cho trước. Tách ra để test xác định được.
func NewAt(t time.Time) string {
	var u [16]byte

	// 48 bit đầu: mili giây từ epoch Unix, big-endian.
	ms := uint64(t.UnixMilli())
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], ms)
	copy(u[0:6], buf[2:8])

	if _, err := rand.Read(u[6:]); err != nil {
		// crypto/rand hỏng nghĩa là hệ thống đang ở tình trạng không thể tin
		// được. Sinh id trùng lặp ở đây sẽ hỏng dữ liệu một cách âm thầm, nên
		// dừng hẳn là lựa chọn đúng.
		panic(fmt.Sprintf("ids: crypto/rand thất bại: %v", err))
	}

	// Phiên bản 7 ở 4 bit cao của byte 6.
	u[6] = (u[6] & 0x0F) | 0x70
	// Biến thể RFC 4122 ở 2 bit cao của byte 8.
	u[8] = (u[8] & 0x3F) | 0x80

	return format(u)
}

func format(u [16]byte) string {
	var out [36]byte
	hex.Encode(out[0:8], u[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], u[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], u[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], u[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], u[10:16])
	return string(out[:])
}
