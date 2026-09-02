package lut

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestConstantsMatchMobile đọc thẳng file TypeScript và khẳng định hằng số layout
// hai bên khớp nhau.
//
// Vì sao cần một test đọc chéo ngôn ngữ như thế này: hằng số hald tồn tại ở hai
// nơi và không có codegen nối chúng. Nếu ai đó đổi HALD_SIZE bên TypeScript mà
// quên bên Go, mọi test khác vẫn xanh — cả hai bên đều tự nhất quán, chỉ là không
// nhất quán VỚI NHAU. Triệu chứng khi đó là màu trên máy khác màu file xuất, và
// không có gì chỉ về nguyên nhân.
//
// Đây là loại lỗi đắt nhất: âm thầm, không crash, chỉ lộ qua khiếu nại của khách.
func TestConstantsMatchMobile(t *testing.T) {
	const path = "../../../../mobile/src/color/haldLut.ts"

	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		// Cho phép build backend riêng lẻ mà không có cây mobile bên cạnh.
		t.Skipf("không tìm thấy %s — bỏ qua kiểm tra chéo", path)
	}
	if err != nil {
		t.Fatalf("đọc %s: %v", path, err)
	}

	want := map[string]int{
		"HALD_SIZE":  HaldSize,
		"HALD_TILES": HaldTiles,
	}
	for name, goValue := range want {
		tsValue, err := extractConst(string(src), name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if tsValue != goValue {
			t.Errorf("%s lệch nhau: TypeScript = %d, Go = %d.\n"+
				"Sửa một bên thì phải sửa cả bên kia VÀ docs/hald-lut-format.md.",
				name, tsValue, goValue)
		}
	}

	// HALD_DIM bên TS là biểu thức tính ra, không phải số hằng, nên kiểm tra
	// gián tiếp qua hai thừa số của nó.
	if HaldDim != HaldSize*HaldTiles {
		t.Errorf("HaldDim = %d nhưng HaldSize*HaldTiles = %d", HaldDim, HaldSize*HaldTiles)
	}
}

func extractConst(src, name string) (int, error) {
	re := regexp.MustCompile(`(?m)^export const ` + regexp.QuoteMeta(name) + `\s*=\s*(\d+)`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		return 0, fmt.Errorf("không tìm thấy khai báo trong file TypeScript")
	}
	return strconv.Atoi(m[1])
}

// TestAdjustmentsMatchMobile đọc thẳng hai file TypeScript và khẳng định phần
// chỉnh màu ở hai bên là MỘT.
//
// Cùng lý do với TestConstantsMatchMobile, nhưng hậu quả nặng hơn: hằng số hald
// sai thì màu lệch đều, còn công thức chỉnh màu lệch thì ảnh khách xem trên máy
// khác ảnh họ nhận về — và chênh lệch chỉ xuất hiện ở những tấm đã kéo slider,
// nên nó không lộ ra ở bất kỳ phép thử nhanh nào.
//
// Test này kiểm hai thứ mà không codegen nào nối giúp: DANH SÁCH tham số, và
// từng HỆ SỐ trong công thức.
func TestAdjustmentsMatchMobile(t *testing.T) {
	const (
		keysPath   = "../../../../mobile/src/color/adjustments.ts"
		shaderPath = "../../../../mobile/src/color/haldLut.ts"
	)

	keysSrc, err := os.ReadFile(keysPath)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("không tìm thấy %s — bỏ qua kiểm tra chéo", keysPath)
	}
	if err != nil {
		t.Fatalf("đọc %s: %v", keysPath, err)
	}
	shaderSrc, err := os.ReadFile(shaderPath)
	if err != nil {
		t.Fatalf("đọc %s: %v", shaderPath, err)
	}

	// --- danh sách tham số ---
	tsKeys, err := extractStringArray(string(keysSrc), "ADJUSTMENT_KEYS")
	if err != nil {
		t.Fatalf("ADJUSTMENT_KEYS: %v", err)
	}

	goFields := reflect.TypeOf(Adjustments{})
	var goKeys []string
	for i := 0; i < goFields.NumField(); i++ {
		goKeys = append(goKeys, strings.ToLower(goFields.Field(i).Name))
	}

	if strings.Join(tsKeys, ",") != strings.Join(goKeys, ",") {
		t.Errorf("danh sách tham số lệch nhau:\n  TypeScript: %v\n  Go:         %v\n"+
			"Thêm hay bỏ một tham số thì phải sửa cả hai bên.", tsKeys, goKeys)
	}

	// --- hệ số trong công thức ---
	checks := []struct {
		name    string
		pattern string
		want    float64
	}{
		{"hệ số nhiệt độ", `c\.r \*= 1\.0 \+ half\(temperature\) \* ([0-9.]+)`, tempGain},
		{"hệ số sắc", `c\.g \*= 1\.0 - half\(tint\) \* ([0-9.]+)`, tintGain},
		{"số khẩu của bù sáng", `exp2\(exposure \* ([0-9.]+)\)`, exposureStops},
		{"hệ số vùng tối", `c \+= half\(shadows\) \* ([0-9.]+)`, toneGain},
		{"hệ số vùng sáng", `c \+= half\(highlights\) \* ([0-9.]+)`, toneGain},
	}
	for _, c := range checks {
		got, err := extractFloat(string(shaderSrc), c.pattern)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s lệch nhau: SkSL = %v, Go = %v", c.name, got, c.want)
		}
	}

	// --- trọng số độ sáng ---
	re := regexp.MustCompile(`LUMA = half3\(([0-9.]+), ([0-9.]+), ([0-9.]+)\)`)
	m := re.FindStringSubmatch(string(shaderSrc))
	if m == nil {
		t.Fatal("không tìm thấy LUMA trong shader")
	}
	wantLuma := []float32{luma.R, luma.G, luma.B}
	for i, name := range []string{"R", "G", "B"} {
		v, err := strconv.ParseFloat(m[i+1], 32)
		if err != nil {
			t.Errorf("LUMA.%s: %v", name, err)
			continue
		}
		if float32(v) != wantLuma[i] {
			t.Errorf("LUMA.%s lệch nhau: SkSL = %v, Go = %v", name, v, wantLuma[i])
		}
	}
}

// extractStringArray đọc một mảng chuỗi hằng trong TypeScript.
func extractStringArray(src, name string) ([]string, error) {
	re := regexp.MustCompile(`(?s)export const ` + regexp.QuoteMeta(name) + `\s*=\s*\[(.*?)\]`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		return nil, fmt.Errorf("không tìm thấy trong TypeScript")
	}
	items := regexp.MustCompile(`'([a-zA-Z]+)'`).FindAllStringSubmatch(m[1], -1)
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it[1])
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("mảng rỗng hoặc không đọc được")
	}
	return out, nil
}

func extractFloat(src, pattern string) (float64, error) {
	m := regexp.MustCompile(pattern).FindStringSubmatch(src)
	if m == nil {
		return 0, fmt.Errorf("không khớp mẫu %s — công thức bên TypeScript đã đổi?", pattern)
	}
	return strconv.ParseFloat(m[1], 64)
}
