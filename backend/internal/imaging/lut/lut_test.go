package lut

import (
	"fmt"
	"image"
	"math"
	"math/rand"
	"strings"
	"testing"
)

// gradientImage sinh ảnh test phủ rộng không gian màu, để Apply không chỉ được
// kiểm tra trên một vài màu may mắn.
func gradientImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o := img.PixOffset(x, y)
			img.Pix[o+0] = uint8(x * 255 / maxInt(w-1, 1))
			img.Pix[o+1] = uint8(y * 255 / maxInt(h-1, 1))
			img.Pix[o+2] = uint8((x + y) * 255 / maxInt(w+h-2, 1))
			img.Pix[o+3] = 255
		}
	}
	return img
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// identityCube sinh một LUT identity kích thước n³ ở dạng văn bản .cube.
func identityCube(n int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "TITLE \"identity %d\"\n", n)
	fmt.Fprintf(&sb, "LUT_3D_SIZE %d\n", n)
	d := float64(n - 1)
	for b := 0; b < n; b++ {
		for g := 0; g < n; g++ {
			for r := 0; r < n; r++ {
				fmt.Fprintf(&sb, "%.8f %.8f %.8f\n",
					float64(r)/d, float64(g)/d, float64(b)/d)
			}
		}
	}
	return sb.String()
}

// warmCube sinh một LUT phi tuyến, không tầm thường: nâng red, hạ blue, và uốn
// green bằng một đường cong. Dùng để test những đường mà identity không chạm tới.
func warmCube(n int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "LUT_3D_SIZE %d\n", n)
	d := float64(n - 1)
	for b := 0; b < n; b++ {
		for g := 0; g < n; g++ {
			for r := 0; r < n; r++ {
				rf, gf, bf := float64(r)/d, float64(g)/d, float64(b)/d
				fmt.Fprintf(&sb, "%.8f %.8f %.8f\n",
					math.Min(1, rf*1.08+0.02),
					math.Pow(gf, 0.95),
					math.Max(0, bf*0.92-0.01))
			}
		}
	}
	return sb.String()
}

func mustParse(t *testing.T, src string) *Cube {
	t.Helper()
	c, err := ParseCube(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseCube: %v", err)
	}
	return c
}

func TestParseCube(t *testing.T) {
	c := mustParse(t, identityCube(17))
	if c.Size != 17 {
		t.Errorf("Size = %d, muốn 17", c.Size)
	}
	if len(c.Data) != 17*17*17 {
		t.Errorf("len(Data) = %d, muốn %d", len(c.Data), 17*17*17)
	}
	if c.Title != "identity 17" {
		t.Errorf("Title = %q, muốn %q", c.Title, "identity 17")
	}
	// Entry cuối của LUT identity phải là trắng.
	last := c.At(16, 16, 16)
	if last.R != 1 || last.G != 1 || last.B != 1 {
		t.Errorf("entry cuối = %+v, muốn {1 1 1}", last)
	}
}

func TestParseCubeRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"thiếu LUT_3D_SIZE": "0.0 0.0 0.0\n",
		"LUT 1D":            "LUT_1D_SIZE 32\n",
		"thiếu entry":       "LUT_3D_SIZE 2\n0 0 0\n1 1 1\n",
		"domain đảo ngược":  "LUT_3D_SIZE 2\nDOMAIN_MIN 1 1 1\nDOMAIN_MAX 0 0 0\n" + strings.Repeat("0 0 0\n", 8),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseCube(strings.NewReader(src)); err == nil {
				t.Fatal("muốn lỗi, nhận được nil")
			}
		})
	}
}

// TestIdentityRoundTrip khẳng định quy ước layout hald là đúng: một LUT identity
// đi qua ToHald rồi SampleHald phải trả về đúng màu đầu vào.
//
// Nếu test này hỏng, gần như chắc chắn là công thức toạ độ trong hald.go hoặc
// trong haldLut.ts đã sai — xem docs/hald-lut-format.md.
func TestIdentityRoundTrip(t *testing.T) {
	hald := mustParse(t, identityCube(HaldSize)).ToHald()
	if err := ValidateHald(hald); err != nil {
		t.Fatalf("ValidateHald: %v", err)
	}

	rng := rand.New(rand.NewSource(1))
	var worst float32
	for i := 0; i < 5000; i++ {
		r, g, b := rng.Float32(), rng.Float32(), rng.Float32()
		got := SampleHald(hald, r, g, b)
		worst = maxf(worst, absf(got.R-r), absf(got.G-g), absf(got.B-b))
	}

	// Ngưỡng một mức 8-bit: identity chỉ có thể lệch do lượng tử hoá entry.
	const tol = 1.0 / 255
	if worst > tol {
		t.Errorf("sai số round-trip = %.5f (%.2f mức 8-bit), vượt ngưỡng %.5f",
			worst, worst*255, tol)
	}
	t.Logf("sai số lớn nhất: %.6f (%.3f mức 8-bit)", worst, worst*255)
}

// TestDeviceServerParity là test quan trọng nhất của package.
//
// Nó ép buộc điều mà docs/hald-lut-format.md hứa hẹn: đường render trên thiết bị
// (hald 8-bit + shader) và đường render của server (lưới float) cho cùng kết quả.
// Không có test này, hai bên sẽ trôi xa nhau âm thầm qua từng lần refactor, và
// triệu chứng chỉ lộ ra khi khách hàng phàn nàn file xuất khác màu preview.
func TestDeviceServerParity(t *testing.T) {
	// 65 là cỡ Resolve hay xuất ra, và là trường hợp duy nhất hald phải GIẢM
	// độ phân giải thay vì tăng — đáng ngờ nhất nên phải có trong test.
	for _, size := range []int{17, 32, 33, HaldSize, 65} {
		t.Run(fmt.Sprintf("cube%d", size), func(t *testing.T) {
			src := mustParse(t, warmCube(size))
			hald := src.ToHald()

			rng := rand.New(rand.NewSource(int64(size)))
			var worst float32
			for i := 0; i < 5000; i++ {
				r, g, b := rng.Float32(), rng.Float32(), rng.Float32()
				device := SampleHald(hald, r, g, b)
				server := src.Sample(r, g, b)
				worst = maxf(worst,
					absf(device.R-server.R),
					absf(device.G-server.G),
					absf(device.B-server.B))
			}

			// Hai nguồn sai lệch: lượng tử hoá 8-bit của entry hald (giới hạn lý
			// thuyết 0,5 mức) và việc hald lấy mẫu lại LUT về 64³ trong khi server
			// dùng lưới gốc. Đo thực tế trên các LUT trong test này: xấu nhất 0,67
			// mức, kể cả với creative look có độ cong cao và LUT 65³ bị giảm xuống.
			// Ngưỡng 1 mức để lại khoảng 33% dư địa.
			const tol = 1.0 / 255
			if worst > tol {
				t.Errorf("lệch thiết bị/server = %.5f (%.2f mức 8-bit), vượt ngưỡng %.5f",
					worst, worst*255, tol)
			}
			t.Logf("lệch lớn nhất: %.6f (%.3f mức 8-bit)", worst, worst*255)
		})
	}
}

func TestApplyAmountZeroIsIdentity(t *testing.T) {
	c := mustParse(t, warmCube(33))
	src := gradientImage(64, 64)

	got := Apply(src, c, 0)
	for i := range src.Pix {
		if got.Pix[i] != src.Pix[i] {
			t.Fatalf("amount=0 phải trả về ảnh gốc, lệch tại byte %d: %d != %d",
				i, got.Pix[i], src.Pix[i])
		}
	}
}

func TestApplyPreservesAlpha(t *testing.T) {
	c := mustParse(t, warmCube(33))
	src := gradientImage(16, 16)
	src.Pix[3] = 42 // đặt alpha khác 255 ở pixel đầu

	got := Apply(src, c, 1)
	if got.Pix[3] != 42 {
		t.Errorf("alpha = %d, muốn 42 — LUT chỉ được tác động lên màu", got.Pix[3])
	}
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func maxf(vs ...float32) float32 {
	m := vs[0]
	for _, v := range vs[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
