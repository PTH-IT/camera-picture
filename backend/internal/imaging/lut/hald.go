package lut

import (
	"fmt"
	"image"
	"math"
)

// Hằng số layout hald. PHẢI khớp với mobile/src/color/haldLut.ts và
// docs/hald-lut-format.md. Sửa ở đây mà không sửa hai chỗ kia thì màu trên
// thiết bị và màu file xuất sẽ lệch nhau — lut_test.go bắt được điều đó.
const (
	// HaldSize là độ phân giải mỗi trục của LUT sau khi chuyển sang hald.
	HaldSize = 64
	// HaldTiles là số tile mỗi hàng. 8×8 = 64 tile, một tile cho mỗi mức blue.
	HaldTiles = 8
	// HaldDim là cạnh của ảnh hald: 64 × 8 = 512.
	//
	// Chọn 512×512 / 8×8 là có chủ đích, không phải tuỳ tiện: cấu hình
	// 1024×1024 với 16×16 tile có báo cáo gây trục trặc khi sample trong Skia
	// runtime effect. 512×512 là cấu hình được dùng thành công rộng rãi nhất.
	HaldDim = HaldSize * HaldTiles
)

// ToHald trải LUT 3D thành ảnh 2D để nạp làm texture GPU.
//
// Cube nguồn có thể ở bất kỳ độ phân giải nào (17, 32, 33, 64 là các giá trị
// thường gặp); hàm này lấy mẫu lại về HaldSize bằng nội suy trilinear, nên
// đầu ra luôn có cùng layout bất kể đầu vào.
func (c *Cube) ToHald() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, HaldDim, HaldDim))
	const denom = float32(HaldSize - 1)

	for b := 0; b < HaldSize; b++ {
		tx := (b % HaldTiles) * HaldSize
		ty := (b / HaldTiles) * HaldSize
		bf := float32(b) / denom

		for g := 0; g < HaldSize; g++ {
			gf := float32(g) / denom
			row := img.PixOffset(tx, ty+g)

			for r := 0; r < HaldSize; r++ {
				v := c.Sample(float32(r)/denom, gf, bf)
				o := row + r*4
				img.Pix[o+0] = quantize8(v.R)
				img.Pix[o+1] = quantize8(v.G)
				img.Pix[o+2] = quantize8(v.B)
				img.Pix[o+3] = 255
			}
		}
	}
	return img
}

// quantize8 chuyển float [0,1] sang 8-bit.
//
// Làm tròn về gần nhất (+0.5) chứ không cắt cụt: cắt cụt tạo sai lệch một chiều
// tích luỹ qua cả LUT, biểu hiện thành ảnh hơi tối đi một cách hệ thống.
func quantize8(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(v*255 + 0.5)
}

// SampleHald tra cứu màu từ ảnh hald, mô phỏng CHÍNH XÁC những gì shader làm:
// bilinear trên trục red/green (phần cứng làm hộ), nội suy tuyến tính thủ công
// trên trục blue.
//
// Hàm này tồn tại để test đối chiếu hai đường render. Đường render thật của
// server dùng Cube.Sample ở độ chính xác float — xem docs/hald-lut-format.md,
// mục "hai dạng, một phép toán".
func SampleHald(img *image.NRGBA, r, g, b float32) RGB {
	r, g, b = clamp01(r), clamp01(g), clamp01(b)

	blue := b * float32(HaldSize-1)
	b0 := float32(math.Floor(float64(blue)))
	b1 := b0 + 1
	if b1 > float32(HaldSize-1) {
		b1 = float32(HaldSize - 1)
	}
	f := blue - b0

	// Ánh xạ red/green vào khoảng giữa tâm texel đầu và tâm texel cuối của tile.
	// Đây là điểm mấu chốt giữ mọi sample nằm hẳn trong tile — xem
	// docs/hald-lut-format.md mục 2.
	rx := 0.5 + r*float32(HaldSize-1)
	gy := 0.5 + g*float32(HaldSize-1)

	s0 := bilinear(img, tileOriginX(b0)+rx, tileOriginY(b0)+gy)
	s1 := bilinear(img, tileOriginX(b1)+rx, tileOriginY(b1)+gy)

	return RGB{
		R: s0.R + (s1.R-s0.R)*f,
		G: s0.G + (s1.G-s0.G)*f,
		B: s0.B + (s1.B-s0.B)*f,
	}
}

func tileOriginX(b float32) float32 {
	return float32(int(b)%HaldTiles) * HaldSize
}

func tileOriginY(b float32) float32 {
	return float32(int(b)/HaldTiles) * HaldSize
}

// bilinear lấy mẫu song tuyến tính với quy ước tâm texel i nằm ở i+0.5,
// giống hệt cách GPU lấy mẫu texture ở chế độ linear filtering.
func bilinear(img *image.NRGBA, x, y float32) RGB {
	fx, fy := x-0.5, y-0.5
	x0 := int(math.Floor(float64(fx)))
	y0 := int(math.Floor(float64(fy)))
	tx := fx - float32(x0)
	ty := fy - float32(y0)

	c00 := texel(img, x0, y0)
	c10 := texel(img, x0+1, y0)
	c01 := texel(img, x0, y0+1)
	c11 := texel(img, x0+1, y0+1)

	top := RGB{
		R: c00.R + (c10.R-c00.R)*tx,
		G: c00.G + (c10.G-c00.G)*tx,
		B: c00.B + (c10.B-c00.B)*tx,
	}
	bot := RGB{
		R: c01.R + (c11.R-c01.R)*tx,
		G: c01.G + (c11.G-c01.G)*tx,
		B: c01.B + (c11.B-c01.B)*tx,
	}
	return RGB{
		R: top.R + (bot.R-top.R)*ty,
		G: top.G + (bot.G-top.G)*ty,
		B: top.B + (bot.B-top.B)*ty,
	}
}

func texel(img *image.NRGBA, x, y int) RGB {
	if x < 0 {
		x = 0
	} else if x >= HaldDim {
		x = HaldDim - 1
	}
	if y < 0 {
		y = 0
	} else if y >= HaldDim {
		y = HaldDim - 1
	}
	o := img.PixOffset(x, y)
	return RGB{
		R: float32(img.Pix[o+0]) / 255,
		G: float32(img.Pix[o+1]) / 255,
		B: float32(img.Pix[o+2]) / 255,
	}
}

// ValidateHald kiểm tra một ảnh có đúng kích thước hald hay không.
//
// Kiểm tra sớm và báo lỗi rõ ràng: nạp nhầm ảnh thường vào chỗ chờ LUT sẽ cho
// ra màu hỏng kỳ quái mà không có thông báo nào, rất tốn thời gian truy vết.
func ValidateHald(img *image.NRGBA) error {
	b := img.Bounds()
	if b.Dx() != HaldDim || b.Dy() != HaldDim {
		return fmt.Errorf("ảnh hald phải là %dx%d, nhận được %dx%d",
			HaldDim, HaldDim, b.Dx(), b.Dy())
	}
	return nil
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
