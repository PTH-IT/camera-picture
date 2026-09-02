package lut

import (
	"image"
	"image/draw"
	"math"
)

// Sample tra cứu màu bằng nội suy trilinear trên lưới LUT, ở độ chính xác float.
//
// Đây là đường render của server. Nó dùng CÙNG phép nội suy với shader trên thiết
// bị (xem SampleHald), chỉ khác ở chỗ không đi qua bước lượng tử hoá 8-bit —
// nên bản giao khách không bị mất chất lượng vô cớ. Sai lệch giữa hai đường được
// ép dưới ngưỡng bằng lut_test.go.
func (c *Cube) Sample(r, g, b float32) RGB {
	// Đưa về [0,1] theo domain khai báo trong file. Phần lớn .cube dùng domain
	// [0,1] nên bước này là no-op, nhưng LUT cho log footage thì không.
	r = clamp01((r - c.DomainMin.R) / (c.DomainMax.R - c.DomainMin.R))
	g = clamp01((g - c.DomainMin.G) / (c.DomainMax.G - c.DomainMin.G))
	b = clamp01((b - c.DomainMin.B) / (c.DomainMax.B - c.DomainMin.B))

	maxIdx := c.Size - 1
	fr := r * float32(maxIdx)
	fg := g * float32(maxIdx)
	fb := b * float32(maxIdx)

	r0, dr := split(fr, maxIdx)
	g0, dg := split(fg, maxIdx)
	b0, db := split(fb, maxIdx)

	r1 := minInt(r0+1, maxIdx)
	g1 := minInt(g0+1, maxIdx)
	b1 := minInt(b0+1, maxIdx)

	// Trilinear: nội suy theo r, rồi g, rồi b.
	c000, c100 := c.At(r0, g0, b0), c.At(r1, g0, b0)
	c010, c110 := c.At(r0, g1, b0), c.At(r1, g1, b0)
	c001, c101 := c.At(r0, g0, b1), c.At(r1, g0, b1)
	c011, c111 := c.At(r0, g1, b1), c.At(r1, g1, b1)

	c00 := lerp(c000, c100, dr)
	c10 := lerp(c010, c110, dr)
	c01 := lerp(c001, c101, dr)
	c11 := lerp(c011, c111, dr)

	c0 := lerp(c00, c10, dg)
	c1 := lerp(c01, c11, dg)

	return lerp(c0, c1, db)
}

// split tách một toạ độ lưới thành chỉ số nguyên và phần lẻ, có kẹp biên.
func split(f float32, maxIdx int) (int, float32) {
	if f <= 0 {
		return 0, 0
	}
	if f >= float32(maxIdx) {
		return maxIdx, 0
	}
	i := int(math.Floor(float64(f)))
	return i, f - float32(i)
}

func lerp(a, b RGB, t float32) RGB {
	return RGB{
		R: a.R + (b.R-a.R)*t,
		G: a.G + (b.G-a.G)*t,
		B: a.B + (b.B-a.B)*t,
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Apply áp LUT lên ảnh và trả về ảnh mới.
//
// Dùng cube gốc ở độ phân giải nguyên bản, không lấy mẫu lại về 64³ để "khớp
// lưới" với hald. Điều đó đã được cân nhắc và bác bỏ: đo trên nhiều LUT (17³,
// 32³, 33³, 65³, gồm cả creative look có độ cong cao và split toning), việc lưới
// server khác lưới hald chỉ gây lệch khoảng 0,2 mức trên thang 8-bit — trong khi
// lấy mẫu lại một LUT 17³ lên 64³ làm số entry tăng 53 lần. Không đáng.
//
// lut_test.go ép buộc kết luận này bằng số. Nếu một LUT thật của khách làm test
// vỡ, cách xử lý là nâng độ phân giải hald, không phải lấy mẫu lại phía server.
//
// amount trong [0,1] là cường độ, khớp với uniform cùng tên trong shader —
// đây là thứ khiến slider trên app và bản render của server cho cùng kết quả.
//
// Lưu ý về độ sâu bit: hàm này nhận vào và trả ra 8-bit, nên chỉ dùng cho thumbnail
// và proxy. Đường xuất bản cuối phải làm việc ở 16-bit qua libvips — áp LUT trên
// dữ liệu 8-bit rồi chỉnh tiếp sẽ tạo posterization không cứu được.
func Apply(src image.Image, c *Cube, amount float32) *image.NRGBA {
	return ApplyGraded(src, c, amount, NeutralAdjustments)
}

// ApplyGraded áp chỉnh màu thủ công RỒI mới tra LUT, đúng thứ tự của shader
// trên thiết bị (xem ADJUSTMENT_SKSL trong mobile/src/color/haldLut.ts).
//
// Thứ tự là phần quan trọng nhất của hàm này: chỉnh tay là hiệu chỉnh cho bản
// chụp, LUT là look phủ lên trên. Áp LUT trước rồi mới sửa sáng sẽ cho ảnh
// khác — và khác cả với thứ người dùng vừa nhìn thấy trên máy.
func ApplyGraded(src image.Image, c *Cube, amount float32, adj Adjustments) *image.NRGBA {
	amount = clamp01(amount)
	b := src.Bounds()

	nrgba, ok := src.(*image.NRGBA)
	if !ok || !nrgba.Rect.Eq(b) {
		conv := image.NewNRGBA(b)
		draw.Draw(conv, b, src, b.Min, draw.Src)
		nrgba = conv
	}

	dst := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		si := nrgba.PixOffset(b.Min.X, y)
		di := dst.PixOffset(b.Min.X, y)
		for x := b.Min.X; x < b.Max.X; x++ {
			sr := float32(nrgba.Pix[si+0]) / 255
			sg := float32(nrgba.Pix[si+1]) / 255
			sb := float32(nrgba.Pix[si+2]) / 255

			// Chỉnh tay chạy trước, LUT tra trên KẾT QUẢ của nó, và `amount`
			// chỉ trộn giữa hai thứ đó — giống hệt dòng cuối của shader.
			if !adj.IsNeutral() {
				a := adj.apply(sr, sg, sb)
				sr, sg, sb = a.R, a.G, a.B
			}

			v := c.Sample(sr, sg, sb)

			dst.Pix[di+0] = quantize8(sr + (v.R-sr)*amount)
			dst.Pix[di+1] = quantize8(sg + (v.G-sg)*amount)
			dst.Pix[di+2] = quantize8(sb + (v.B-sb)*amount)
			// Alpha đi qua nguyên vẹn: LUT tác động lên màu, không lên độ trong.
			dst.Pix[di+3] = nrgba.Pix[si+3]

			si += 4
			di += 4
		}
	}
	return dst
}
