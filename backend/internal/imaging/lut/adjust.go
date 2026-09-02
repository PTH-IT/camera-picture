package lut

import "math"

// Adjustments là phần chỉnh màu thủ công của một tấm ảnh.
//
// Bản đối ứng phía thiết bị: mobile/src/color/adjustments.ts và khối
// ADJUSTMENT_SKSL trong mobile/src/color/haldLut.ts. Hai bên PHẢI cho cùng kết
// quả, và adjust_test.go đọc thẳng file TypeScript để ép buộc điều đó — nếu
// không, ảnh khách nhìn trên máy sẽ khác ảnh họ nhận về, mà không có gì báo lỗi.
//
// Mọi giá trị nằm trong [-1, 1] và 0 là KHÔNG ĐỔI GÌ.
type Adjustments struct {
	// Exposure bù sáng, ±2 khẩu.
	Exposure float32
	// Contrast quanh điểm giữa 0.5.
	Contrast float32
	// Saturation: -1 là đen trắng hoàn toàn.
	Saturation float32
	// Temperature: âm là lạnh, dương là ấm.
	Temperature float32
	// Tint: âm ngả lục, dương ngả tím.
	Tint float32
	// Highlights kéo vùng sáng.
	Highlights float32
	// Shadows mở vùng tối.
	Shadows float32
}

// Hệ số của từng phép. Tách thành hằng số có tên vì adjust_test.go so từng con
// số này với bản SkSL — một hằng số viết thẳng trong công thức thì không so được.
const (
	tempGain      = 0.25
	tintGain      = 0.15
	exposureStops = 2.0
	toneGain      = 0.4
)

// Trọng số độ sáng Rec. 709. Phải khớp LUMA trong ADJUSTMENT_SKSL.
var luma = RGB{R: 0.2126, G: 0.7152, B: 0.0722}

// NeutralAdjustments không đổi gì. Dùng nó thay vì Adjustments{} ở chỗ cần nói
// rõ ý định.
var NeutralAdjustments = Adjustments{}

// IsNeutral cho biết có phép nào cần chạy không.
func (a Adjustments) IsNeutral() bool {
	return a == Adjustments{}
}

// clampParams kẹp mọi tham số về [-1,1].
//
// Dữ liệu này đến từ `overrides` do client ghi, tức là từ ngoài vào. Một số
// ngoài biên không làm hàm nào lỗi — nó chỉ cho ra ảnh cháy trắng, và triệu
// chứng đó không chỉ về nguyên nhân.
func (a Adjustments) clampParams() Adjustments {
	return Adjustments{
		Exposure:    clampSigned(a.Exposure),
		Contrast:    clampSigned(a.Contrast),
		Saturation:  clampSigned(a.Saturation),
		Temperature: clampSigned(a.Temperature),
		Tint:        clampSigned(a.Tint),
		Highlights:  clampSigned(a.Highlights),
		Shadows:     clampSigned(a.Shadows),
	}
}

// apply chạy đúng thứ tự của shader trên thiết bị.
//
// Thứ tự không phải tuỳ ý: cân bằng trắng và bù sáng là hiệu chỉnh cho bản chụp,
// nên chúng chạy trước; tương phản và vùng sáng/tối làm việc trên kết quả đó.
// Đảo thứ tự cho ra ảnh khác, và khác cả với thiết bị.
func (a Adjustments) apply(r, g, b float32) RGB {
	a = a.clampParams()

	// 1. Cân bằng trắng.
	r *= 1 + a.Temperature*tempGain
	b *= 1 - a.Temperature*tempGain
	g *= 1 - a.Tint*tintGain

	// 2. Bù sáng theo KHẨU, không cộng tuyến tính: cộng thẳng làm bệt vùng tối
	//    và cháy vùng sáng.
	gain := float32(math.Exp2(float64(a.Exposure * exposureStops)))
	r *= gain
	g *= gain
	b *= gain

	// 3. Tương phản quanh điểm giữa.
	k := 1 + a.Contrast
	r = (r-0.5)*k + 0.5
	g = (g-0.5)*k + 0.5
	b = (b-0.5)*k + 0.5

	// 4. Vùng sáng và vùng tối, trọng số bình phương theo độ sáng để không đụng
	//    vào vùng trung tính.
	l := dotLuma(clamp01(r), clamp01(g), clamp01(b))
	lift := a.Shadows*toneGain*(1-l)*(1-l) + a.Highlights*toneGain*l*l
	r += lift
	g += lift
	b += lift

	// 5. Bão hoà, trộn về mức xám cùng độ sáng.
	grey := dotLuma(clamp01(r), clamp01(g), clamp01(b))
	s := 1 + a.Saturation
	r = grey + (r-grey)*s
	g = grey + (g-grey)*s
	b = grey + (b-grey)*s

	return RGB{R: clamp01(r), G: clamp01(g), B: clamp01(b)}
}

func dotLuma(r, g, b float32) float32 {
	return r*luma.R + g*luma.G + b*luma.B
}

func clampSigned(v float32) float32 {
	if v < -1 {
		return -1
	}
	if v > 1 {
		return 1
	}
	return v
}

// AdjustmentsFrom đọc chỉnh màu từ `overrides` của một bản ghi chỉnh sửa.
//
// Bỏ qua mọi khoá lạ và mọi giá trị không phải số: `overrides` là jsonb tự do
// do client ghi, và một bản app cũ hơn hoàn toàn có thể để lại thứ khác trong
// đó. Bỏ qua thì ảnh chỉ mất một phép chỉnh; ném lỗi thì cả việc xuất ảnh hỏng.
func AdjustmentsFrom(overrides map[string]any) Adjustments {
	var a Adjustments
	set := map[string]*float32{
		"exposure":    &a.Exposure,
		"contrast":    &a.Contrast,
		"saturation":  &a.Saturation,
		"temperature": &a.Temperature,
		"tint":        &a.Tint,
		"highlights":  &a.Highlights,
		"shadows":     &a.Shadows,
	}
	for key, dst := range set {
		if v, ok := numberOf(overrides[key]); ok {
			*dst = clampSigned(v)
		}
	}
	return a
}

// numberOf chấp nhận cả float64 (đường JSON) lẫn float32 (gọi trực tiếp).
func numberOf(v any) (float32, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return float32(n), true
	case float32:
		if math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
			return 0, false
		}
		return n, true
	case int:
		return float32(n), true
	default:
		return 0, false
	}
}
