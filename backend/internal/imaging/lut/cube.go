// Package lut đọc LUT 3D định dạng .cube và chuyển sang hald PNG cho GPU.
//
// Quy ước layout hald và công thức tra cứu được đặc tả ở docs/hald-lut-format.md.
// Bản implementation phía thiết bị nằm ở mobile/src/color/haldLut.ts. Hai bên phải
// cho cùng kết quả — lut_test.go ép buộc điều đó.
package lut

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// RGB là một entry LUT ở dạng float.
//
// Dùng float32 chứ không phải float64: một LUT 64³ là 262144 entry, và file .cube
// nguồn vốn chỉ có khoảng 6 chữ số thập phân — float64 chỉ tăng gấp đôi bộ nhớ mà
// không thêm độ chính xác có thật.
type RGB struct{ R, G, B float32 }

// Cube là một LUT 3D đã parse từ file .cube.
type Cube struct {
	Title     string
	Size      int
	DomainMin RGB
	DomainMax RGB
	// Data theo đúng thứ tự của .cube: R biến thiên nhanh nhất, rồi G, rồi B.
	// Index của (r,g,b) là r + g*Size + b*Size*Size.
	Data []RGB
}

// index trả về vị trí của entry (r,g,b) trong Data.
func (c *Cube) index(r, g, b int) int {
	return r + g*c.Size + b*c.Size*c.Size
}

// At trả về entry tại toạ độ lưới nguyên. Gọi với toạ độ ngoài lưới là lỗi lập
// trình, nên hàm này panic thay vì trả lỗi — mọi caller trong package đều đã clamp.
func (c *Cube) At(r, g, b int) RGB {
	return c.Data[c.index(r, g, b)]
}

// ParseCube đọc định dạng .cube của Adobe/Resolve.
//
// Chỉ hỗ trợ LUT_3D_SIZE. LUT 1D bị từ chối tường minh thay vì bỏ qua âm thầm:
// một file 1D parse "thành công" rồi cho ra màu sai là kiểu lỗi tốn hàng giờ để
// truy vết.
func ParseCube(r io.Reader) (*Cube, error) {
	c := &Cube{
		DomainMin: RGB{0, 0, 0},
		DomainMax: RGB{1, 1, 1},
	}

	sc := bufio.NewScanner(r)
	// File .cube 64³ có hơn 260 nghìn dòng nhưng mỗi dòng đều ngắn; nới buffer
	// chỉ để phòng file có dòng TITLE dài bất thường.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lineNo := 0
	for sc.Scan() {
		lineNo++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		fields := strings.Fields(text)
		keyword := strings.ToUpper(fields[0])

		switch keyword {
		case "TITLE":
			c.Title = strings.Trim(strings.TrimSpace(text[len(fields[0]):]), `"`)

		case "LUT_3D_SIZE":
			if len(fields) < 2 {
				return nil, fmt.Errorf("dòng %d: LUT_3D_SIZE thiếu giá trị", lineNo)
			}
			n, err := strconv.Atoi(fields[1])
			if err != nil {
				return nil, fmt.Errorf("dòng %d: LUT_3D_SIZE không phải số: %w", lineNo, err)
			}
			if n < 2 || n > 256 {
				return nil, fmt.Errorf("dòng %d: LUT_3D_SIZE = %d nằm ngoài khoảng hợp lệ 2..256", lineNo, n)
			}
			c.Size = n
			c.Data = make([]RGB, 0, n*n*n)

		case "LUT_1D_SIZE":
			return nil, fmt.Errorf("dòng %d: LUT 1D không được hỗ trợ, cần LUT 3D", lineNo)

		case "DOMAIN_MIN", "DOMAIN_MAX":
			v, err := parseTriple(fields[1:])
			if err != nil {
				return nil, fmt.Errorf("dòng %d: %s: %w", lineNo, keyword, err)
			}
			if keyword == "DOMAIN_MIN" {
				c.DomainMin = v
			} else {
				c.DomainMax = v
			}

		default:
			// Mọi thứ còn lại phải là một dòng dữ liệu ba số.
			if c.Size == 0 {
				return nil, fmt.Errorf("dòng %d: gặp dữ liệu trước khi khai báo LUT_3D_SIZE", lineNo)
			}
			v, err := parseTriple(fields)
			if err != nil {
				return nil, fmt.Errorf("dòng %d: %w", lineNo, err)
			}
			c.Data = append(c.Data, v)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("đọc file: %w", err)
	}

	if c.Size == 0 {
		return nil, fmt.Errorf("thiếu LUT_3D_SIZE")
	}
	want := c.Size * c.Size * c.Size
	if len(c.Data) != want {
		return nil, fmt.Errorf("số entry không khớp: có %d, cần %d cho LUT %d³",
			len(c.Data), want, c.Size)
	}
	if c.DomainMax.R <= c.DomainMin.R || c.DomainMax.G <= c.DomainMin.G || c.DomainMax.B <= c.DomainMin.B {
		return nil, fmt.Errorf("DOMAIN_MAX phải lớn hơn DOMAIN_MIN")
	}

	return c, nil
}

func parseTriple(fields []string) (RGB, error) {
	if len(fields) < 3 {
		return RGB{}, fmt.Errorf("cần 3 số, có %d", len(fields))
	}
	var out [3]float32
	for i := 0; i < 3; i++ {
		f, err := strconv.ParseFloat(fields[i], 32)
		if err != nil {
			return RGB{}, fmt.Errorf("giá trị %q không phải số: %w", fields[i], err)
		}
		out[i] = float32(f)
	}
	return RGB{out[0], out[1], out[2]}, nil
}
