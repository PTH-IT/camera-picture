package storage

import (
	"strings"
	"testing"
	"time"

	"github.com/hauph/camera/backend/internal/protocol"
)

func TestSessionFolderPutsDateFirst(t *testing.T) {
	day := time.Date(2026, 8, 30, 9, 30, 0, 0, time.UTC)

	if got := SessionFolder(day, "Minh & Lan"); got != "2026-08-30 Minh & Lan" {
		t.Errorf("nhận %q", got)
	}
	// Không có tên thì chỉ còn ngày — không để lại khoảng trắng thừa ở cuối.
	if got := SessionFolder(day, "   "); got != "2026-08-30" {
		t.Errorf("tên rỗng: nhận %q", got)
	}
}

// TestSanitizeBlocksPathEscape là phép kiểm quan trọng nhất của file này.
//
// Tên buổi chụp và tên file đều do người dùng đặt. Cho dấu gạch chéo đi qua
// nghĩa là họ tạo được cấp thư mục mới, và ".." leo ngược lên trên thư mục gốc —
// tức là ghi được ra ngoài phạm vi của chính mình.
func TestSanitizeBlocksPathEscape(t *testing.T) {
	day := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)

	cases := []string{
		"../..",
		"a/b/c",
		"..\\..\\windows",
		"C:/Users",
	}
	for _, name := range cases {
		key := KeyFor(day, name, protocol.TierOriginal, "DSC_4001.NEF")
		for _, seg := range strings.Split(key, "/") {
			if seg == "." || seg == ".." || seg == "" {
				t.Errorf("tên %q tạo ra đoạn đường dẫn nguy hiểm %q trong khoá %q", name, seg, key)
			}
		}
		// Khoá luôn phải có đúng ba đoạn: buổi chụp / thư mục con / tên file.
		if n := strings.Count(key, "/"); n != 2 {
			t.Errorf("tên %q làm khoá có %d dấu gạch chéo: %q", name, n, key)
		}
	}
}

func TestFolderForTier(t *testing.T) {
	cases := map[protocol.AssetTier]string{
		protocol.TierOriginal: FolderOriginals,
		protocol.TierExport:   FolderEdited,
		protocol.TierPreview:  FolderPreviews,
		protocol.TierThumb:    FolderPreviews,
		protocol.TierProxy:    FolderPreviews,
	}
	for tier, want := range cases {
		if got := FolderForTier(tier); got != want {
			t.Errorf("tier %q: nhận %q, mong đợi %q", tier, got, want)
		}
	}
}

// TestKeyForShape: bản gốc và bản đã chỉnh của CÙNG một ảnh nằm cạnh nhau trong
// cùng thư mục cha, khác nhau đúng ở thư mục con.
func TestKeyForShape(t *testing.T) {
	day := time.Date(2026, 8, 30, 15, 0, 0, 0, time.UTC)

	orig := KeyFor(day, "Minh & Lan", protocol.TierOriginal, "DSC_4001.NEF")
	edit := KeyFor(day, "Minh & Lan", protocol.TierExport, "DSC_4001.jpg")

	if orig != "2026-08-30 Minh & Lan/goc/DSC_4001.NEF" {
		t.Errorf("khoá ảnh gốc: %q", orig)
	}
	if edit != "2026-08-30 Minh & Lan/da-chinh/DSC_4001.jpg" {
		t.Errorf("khoá ảnh đã chỉnh: %q", edit)
	}
}

func TestSanitizeTrimsAndCollapses(t *testing.T) {
	day := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)

	// Khoảng trắng liên tiếp gộp lại; ký tự điều khiển bị bỏ.
	got := SessionFolder(day, "  Minh\t\t&\nLan  \x00")
	if got != "2026-08-30 Minh & Lan" {
		t.Errorf("nhận %q", got)
	}

	// Tên dài bị cắt, và không được để lại khoảng trắng ở cuối sau khi cắt.
	long := SessionFolder(day, strings.Repeat("a", 200))
	if len(long) > len("2026-08-30 ")+maxFolderNameLen {
		t.Errorf("tên dài không bị cắt: %d ký tự", len(long))
	}
	if strings.HasSuffix(long, " ") {
		t.Error("cắt xong còn khoảng trắng ở cuối")
	}
}
