package ids

import (
	"regexp"
	"sort"
	"testing"
	"time"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// TestFormatIsValidUUIDv7 quan trọng vì lược đồ khai cột kiểu uuid. Sinh ra chuỗi
// không đúng định dạng thì mọi lệnh chèn đều lỗi — và lỗi đó chỉ lộ ra lúc chạy
// với Postgres thật, không lộ khi test bằng store trong bộ nhớ.
func TestFormatIsValidUUIDv7(t *testing.T) {
	for i := 0; i < 200; i++ {
		id := New()
		if !uuidRe.MatchString(id) {
			t.Fatalf("id %q không đúng định dạng UUID v7", id)
		}
	}
}

func TestUnique(t *testing.T) {
	seen := make(map[string]bool, 10000)
	for i := 0; i < 10000; i++ {
		id := New()
		if seen[id] {
			t.Fatalf("id trùng: %q", id)
		}
		seen[id] = true
	}
}

// TestSortsByTime là lý do chọn v7 thay vì v4.
//
// Id sinh theo thời gian tăng dần phải sắp xếp theo thứ tự chuỗi trùng với thứ tự
// thời gian. Nhờ đó các dòng chèn gần nhau nằm gần nhau trong chỉ mục B-tree,
// thay vì rơi vào các trang ngẫu nhiên như với v4.
func TestSortsByTime(t *testing.T) {
	base := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	var got []string
	for i := 0; i < 50; i++ {
		got = append(got, NewAt(base.Add(time.Duration(i)*time.Second)))
	}

	sorted := append([]string(nil), got...)
	sort.Strings(sorted)

	for i := range got {
		if got[i] != sorted[i] {
			t.Fatalf("thứ tự chuỗi không khớp thứ tự thời gian tại vị trí %d", i)
		}
	}
}

// TestTimestampIsEmbedded: 48 bit đầu phải là mili giây Unix, nếu không thì tính
// sắp xếp theo thời gian chỉ là tình cờ.
func TestTimestampIsEmbedded(t *testing.T) {
	want := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	id := NewAt(want)

	// 12 ký tự hex đầu (bỏ dấu gạch) = 48 bit thời gian.
	hexTime := id[0:8] + id[9:13]
	var ms int64
	for _, c := range hexTime {
		var v int64
		switch {
		case c >= '0' && c <= '9':
			v = int64(c - '0')
		case c >= 'a' && c <= 'f':
			v = int64(c-'a') + 10
		default:
			t.Fatalf("ký tự không phải hex: %q", c)
		}
		ms = ms*16 + v
	}

	if ms != want.UnixMilli() {
		t.Errorf("dấu thời gian nhúng = %d, muốn %d", ms, want.UnixMilli())
	}
}
