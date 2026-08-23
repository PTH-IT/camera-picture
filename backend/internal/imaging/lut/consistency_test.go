package lut

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strconv"
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
