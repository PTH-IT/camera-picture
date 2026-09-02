package storage

import (
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/hauph/camera/backend/internal/protocol"
)

// Cấu trúc thư mục trong kho lưu trữ, giống nhau ở mọi nhà cung cấp:
//
//	2026-08-30 Minh & Lan/
//	  goc/          ảnh gốc lấy từ máy ảnh
//	  da-chinh/     ảnh đã chỉnh, bản giao khách
//	  xem-truoc/    preview và thumbnail
//
// Vì sao thư mục cha theo NGÀY CHỤP chứ không theo id buổi chụp: người dùng mở
// Drive bằng trình duyệt và phải tự tìm được buổi chụp của mình. Một uuid trong
// tên thư mục là vô nghĩa với họ.
//
// Vì sao tách gốc và đã chỉnh: đó là hai thứ khác nhau về mục đích. Ảnh gốc là
// bản lưu trữ, ảnh đã chỉnh là bản giao khách — trộn chung một thư mục thì việc
// gửi cho khách trở thành việc lọc tay giữa hàng nghìn file.
const (
	FolderOriginals = "goc"
	FolderEdited    = "da-chinh"
	FolderPreviews  = "xem-truoc"
)

// maxFolderNameLen giữ tên thư mục ở mức đọc được.
//
// Drive cho tới 32767 ký tự, nhưng một tên dài hơn dòng chữ trên màn hình thì
// không ai đọc được, và một số hệ thống lưu trữ khác vẫn còn giới hạn đường dẫn.
const maxFolderNameLen = 60

// SessionFolder dựng tên thư mục cha cho một buổi chụp.
//
// Ngày luôn đứng trước để thư mục tự sắp theo thứ tự thời gian khi liệt kê —
// đó là thứ tự người dùng tìm kiếm trong đầu.
//
// Tên buổi chụp được ghép vào khi có, vì hai buổi chụp trong cùng một ngày là
// chuyện bình thường (sáng cưới, chiều kỷ yếu) và gộp chúng vào một thư mục sẽ
// trộn ảnh của hai khách hàng.
func SessionFolder(startedAt time.Time, name string) string {
	day := startedAt.Format("2006-01-02")
	clean := sanitizeSegment(name)
	if clean == "" {
		return day
	}
	return day + " " + clean
}

// FolderForTier ánh xạ tầng asset sang thư mục con.
//
// Rẽ nhánh ở ĐÚNG MỘT CHỖ này. Rải `if tier == ...` ra nhiều nơi là cách chắc
// chắn để một đường ghi file rơi nhầm thư mục, và khi phát hiện thì đã có hàng
// nghìn file nằm sai chỗ.
func FolderForTier(tier protocol.AssetTier) string {
	switch tier {
	case protocol.TierOriginal:
		return FolderOriginals
	case protocol.TierExport:
		return FolderEdited
	default:
		// thumb, preview, proxy: đều là bản phụ trợ để xem, không phải bản giao.
		return FolderPreviews
	}
}

// ObjectKey dựng khoá đầy đủ của một file.
//
// Với S3/MinIO đây là khoá object (dấu gạch chéo chỉ là quy ước hiển thị); với
// Drive, mỗi đoạn là một thư mục thật. Cả hai dùng chung một chuỗi để đường dẫn
// nhìn thấy trong Drive khớp với khoá lưu trong cơ sở dữ liệu.
func ObjectKey(sessionFolder, folder, filename string) string {
	return path.Join(sanitizeSegment(sessionFolder), folder, sanitizeSegment(filename))
}

// KeyFor là đường tắt cho trường hợp thường gặp nhất.
func KeyFor(startedAt time.Time, sessionName string, tier protocol.AssetTier, filename string) string {
	return ObjectKey(SessionFolder(startedAt, sessionName), FolderForTier(tier), filename)
}

// sanitizeSegment làm sạch một đoạn đường dẫn.
//
// Ba thứ bắt buộc phải chặn, và cả ba đều bắt nguồn từ dữ liệu do NGƯỜI DÙNG
// đặt (tên buổi chụp, tên file trên thẻ):
//
//  1. Dấu gạch chéo — cho phép đi thì tên buổi chụp "a/b" tạo thêm một cấp thư
//     mục, và ".." leo ngược lên trên cả thư mục gốc.
//  2. Ký tự điều khiển — chúng đi qua HTTP nhưng làm hỏng tên file ở đầu kia.
//  3. Khoảng trắng thừa ở hai đầu — Drive giữ nguyên và người dùng thấy một tên
//     thụt lề vô cớ.
func sanitizeSegment(s string) string {
	var b strings.Builder
	lastSpace, lastDot := false, false
	for _, r := range s {
		switch {
		case r == '/' || r == '\\' || r == ':':
			// Thay bằng gạch ngang thay vì bỏ hẳn: "Minh/Lan" thành "Minh-Lan"
			// vẫn đọc được, còn "MinhLan" thì mất thông tin.
			b.WriteRune('-')
			lastSpace, lastDot = false, false

		// Khoảng trắng phải xét TRƯỚC ký tự điều khiển: tab và xuống dòng thoả
		// cả hai, và nếu rơi vào nhánh điều khiển thì "Minh\tLan" dính thành
		// "MinhLan" — mất luôn ranh giới giữa hai từ.
		case unicode.IsSpace(r):
			if !lastSpace {
				b.WriteRune(' ')
			}
			lastSpace, lastDot = true, false

		case unicode.IsControl(r):
			// bỏ

		case r == '.':
			// Gộp chuỗi dấu chấm về một: ".." trong tên là vô hại với path.Join
			// nhưng vẫn là thứ không ai cố ý đặt, và nó làm tên khó đọc.
			if !lastDot {
				b.WriteRune('.')
			}
			lastDot, lastSpace = true, false

		default:
			b.WriteRune(r)
			lastSpace, lastDot = false, false
		}
	}

	// Cắt sạch dấu chấm, gạch ngang và khoảng trắng ở hai đầu: tên bắt đầu bằng
	// dấu chấm bị ẩn trên hệ tệp kiểu Unix, và một cái tên mở đầu bằng gạch
	// ngang trông như lỗi.
	out := strings.Trim(b.String(), " .-")
	if len(out) > maxFolderNameLen {
		out = strings.Trim(out[:maxFolderNameLen], " .-")
	}
	return out
}
