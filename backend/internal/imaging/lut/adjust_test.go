package lut

import (
	"image"
	"math"
	"testing"
)

func adjusted(a Adjustments, r, g, b float32) RGB { return a.apply(r, g, b) }

// TestNeutralIsIdentity: `overrides` rỗng và `overrides` toàn số 0 phải cho ra
// cùng một ảnh.
//
// Nếu không, ảnh sẽ đổi màu chỉ vì người dùng chạm vào thanh trượt rồi kéo về
// chỗ cũ — và bản render của máy chủ sẽ lệch khỏi thiết bị đúng bằng lượng đó.
func TestNeutralIsIdentity(t *testing.T) {
	for _, v := range []float32{0, 0.25, 0.5, 0.75, 1} {
		got := adjusted(NeutralAdjustments, v, v/2, 1-v)
		if got.R != v || got.G != v/2 || got.B != 1-v {
			t.Errorf("trung tính làm đổi màu: vào (%v,%v,%v), ra %+v", v, v/2, 1-v, got)
		}
	}
	if !NeutralAdjustments.IsNeutral() {
		t.Error("NeutralAdjustments.IsNeutral() = false")
	}
}

// TestExposureIsStops: bù sáng phải là phép NHÂN theo khẩu, không phải cộng.
//
// Cộng tuyến tính làm bệt vùng tối và cháy vùng sáng; nhân giữ nguyên tương
// quan giữa các vùng, đúng như bù sáng trên máy ảnh.
func TestExposureIsStops(t *testing.T) {
	cases := []struct {
		exposure float32
		in, want float32
	}{
		{0.5, 0.25, 0.5},  // +1 khẩu
		{-0.5, 0.5, 0.25}, // -1 khẩu
		{1, 0.1, 0.4},     // +2 khẩu, trần của thang
		{-1, 0.8, 0.2},    // -2 khẩu
	}
	for _, tc := range cases {
		got := adjusted(Adjustments{Exposure: tc.exposure}, tc.in, tc.in, tc.in)
		if absf(got.R-tc.want) > 1e-4 {
			t.Errorf("exposure %v trên %v: nhận %v, mong đợi %v", tc.exposure, tc.in, got.R, tc.want)
		}
	}
}

// TestSaturationMinusOneIsGrey: -1 phải cho ra xám THẬT, cùng độ sáng.
func TestSaturationMinusOneIsGrey(t *testing.T) {
	got := adjusted(Adjustments{Saturation: -1}, 0.9, 0.3, 0.1)
	if absf(got.R-got.G) > 1e-5 || absf(got.G-got.B) > 1e-5 {
		t.Fatalf("không ra xám: %+v", got)
	}
	want := dotLuma(0.9, 0.3, 0.1)
	if absf(got.R-want) > 1e-4 {
		t.Errorf("độ sáng lệch: nhận %v, mong đợi %v", got.R, want)
	}
}

// TestTemperatureWarmsAndCools: ấm thì đỏ lên lam xuống, lạnh thì ngược lại.
func TestTemperatureWarmsAndCools(t *testing.T) {
	warm := adjusted(Adjustments{Temperature: 1}, 0.5, 0.5, 0.5)
	if !(warm.R > 0.5 && warm.B < 0.5) {
		t.Errorf("ấm phải tăng đỏ giảm lam: %+v", warm)
	}
	cool := adjusted(Adjustments{Temperature: -1}, 0.5, 0.5, 0.5)
	if !(cool.R < 0.5 && cool.B > 0.5) {
		t.Errorf("lạnh phải giảm đỏ tăng lam: %+v", cool)
	}
}

// TestShadowsHitDarkMoreThanBright: vùng tối phải tác động theo độ sáng, không
// phải cộng đều cả ảnh — cộng đều là "độ sáng", không phải "vùng tối".
func TestShadowsHitDarkMoreThanBright(t *testing.T) {
	a := Adjustments{Shadows: 1}
	dark := adjusted(a, 0.05, 0.05, 0.05).R - 0.05
	bright := adjusted(a, 0.9, 0.9, 0.9).R - 0.9
	if !(dark > bright*3) {
		t.Errorf("vùng tối tác động %v, vùng sáng %v — chênh lệch quá nhỏ", dark, bright)
	}

	h := Adjustments{Highlights: -1}
	hiDrop := 0.9 - adjusted(h, 0.9, 0.9, 0.9).R
	loDrop := 0.05 - adjusted(h, 0.05, 0.05, 0.05).R
	if !(hiDrop > loDrop*3) {
		t.Errorf("vùng sáng giảm %v, vùng tối giảm %v — chênh lệch quá nhỏ", hiDrop, loDrop)
	}
}

// TestParamsAreClamped: số ngoài [-1,1] không được cho ra kết quả khác ±1.
//
// Dữ liệu đến từ `overrides` do client ghi. Số ngoài biên không làm hàm nào lỗi,
// nó chỉ cho ra ảnh cháy trắng — và triệu chứng đó không chỉ về nguyên nhân.
func TestParamsAreClamped(t *testing.T) {
	huge := adjusted(Adjustments{Exposure: 99}, 0.2, 0.2, 0.2)
	max := adjusted(Adjustments{Exposure: 1}, 0.2, 0.2, 0.2)
	if huge != max {
		t.Errorf("exposure 99 cho %+v, exposure 1 cho %+v — phải như nhau", huge, max)
	}
}

func TestAdjustmentsFromIgnoresJunk(t *testing.T) {
	a := AdjustmentsFrom(map[string]any{
		"exposure":   0.4,          // float64, đường JSON
		"contrast":   float32(0.2), // gọi trực tiếp
		"saturation": "nhiều",      // rác: bỏ qua
		"tint":       math.NaN(),   // rác: bỏ qua
		"shadows":    5.0,          // ngoài biên: kẹp
		"khoa_la":    1.0,          // khoá lạ: bỏ qua
	})

	if absf(a.Exposure-0.4) > 1e-6 || absf(a.Contrast-0.2) > 1e-6 {
		t.Errorf("số hợp lệ không qua được: %+v", a)
	}
	if a.Saturation != 0 || a.Tint != 0 {
		t.Errorf("giá trị rác lọt vào: %+v", a)
	}
	if a.Shadows != 1 {
		t.Errorf("không kẹp biên: shadows = %v", a.Shadows)
	}
	if AdjustmentsFrom(nil) != NeutralAdjustments {
		t.Error("overrides rỗng phải là trung tính")
	}
}

// TestApplyGradedRunsAdjustmentsBeforeLUT khẳng định THỨ TỰ, thứ dễ đảo nhất.
//
// Dùng amount = 0: khi đó LUT không có tác dụng và ảnh ra phải bằng đúng phần
// chỉnh tay — giống hệt dòng cuối của shader trên thiết bị.
func TestApplyGradedRunsAdjustmentsBeforeLUT(t *testing.T) {
	c := mustParse(t, identityCube(17))
	src := gradientImage(16, 16)
	adj := Adjustments{Exposure: 0.5, Saturation: -1}

	out := ApplyGraded(src, c, 0, adj)
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			si := src.PixOffset(x, y)
			want := adj.apply(
				float32(src.Pix[si+0])/255,
				float32(src.Pix[si+1])/255,
				float32(src.Pix[si+2])/255,
			)
			di := out.PixOffset(x, y)
			if out.Pix[di+0] != quantize8(want.R) || out.Pix[di+1] != quantize8(want.G) {
				t.Fatalf("pixel (%d,%d): nhận (%d,%d), mong đợi (%d,%d)",
					x, y, out.Pix[di+0], out.Pix[di+1], quantize8(want.R), quantize8(want.G))
			}
		}
	}
}

// TestApplyKeepsWorkingWithoutAdjustments: Apply cũ phải y hệt như trước.
func TestApplyKeepsWorkingWithoutAdjustments(t *testing.T) {
	c := mustParse(t, warmCube(17))
	src := gradientImage(8, 8)

	old := Apply(src, c, 0.7)
	graded := ApplyGraded(src, c, 0.7, NeutralAdjustments)
	if !imagesEqual(old, graded) {
		t.Error("Apply và ApplyGraded với chỉnh màu trung tính cho kết quả khác nhau")
	}
}

func imagesEqual(a, b *image.NRGBA) bool {
	if !a.Rect.Eq(b.Rect) || len(a.Pix) != len(b.Pix) {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}
