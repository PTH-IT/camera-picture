// Command lutconv chuyển LUT .cube sang ảnh hald PNG để nạp làm texture GPU.
//
// Đây là NGUỒN SỰ THẬT DUY NHẤT sinh ra LUT cho app. Nếu ai đó chuyển đổi bằng
// công cụ khác — Photoshop, script online, plugin nào đó — layout có thể khác và
// màu trên thiết bị sẽ lệch với màu server render, mà không có thông báo lỗi nào.
// Luôn dùng lệnh này.
//
//	lutconv -out mobile/assets/luts luts/*.cube
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/hauph/camera/backend/internal/imaging/lut"
)

func main() {
	outDir := flag.String("out", ".", "thư mục ghi file PNG")
	verbose := flag.Bool("v", false, "in chi tiết từng file")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Chuyển LUT .cube sang hald PNG %dx%d.\n\n", lut.HaldDim, lut.HaldDim)
		fmt.Fprintf(os.Stderr, "Cách dùng:\n  lutconv [-out DIR] [-v] file.cube [file.cube ...]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	inputs := flag.Args()
	if len(inputs) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal("tạo thư mục đích: %v", err)
	}

	var failed int
	for _, in := range inputs {
		outPath, err := convert(in, *outDir)
		if err != nil {
			// Không dừng ở file đầu tiên bị lỗi: khi chuyển cả một thư mục preset,
			// biết TẤT CẢ file nào hỏng trong một lần chạy tiện hơn nhiều so với
			// sửa từng cái rồi chạy lại.
			fmt.Fprintf(os.Stderr, "LỖI  %s: %v\n", in, err)
			failed++
			continue
		}
		if *verbose {
			fmt.Printf("OK   %s -> %s\n", in, outPath)
		}
	}

	if failed > 0 {
		fatal("%d/%d file thất bại", failed, len(inputs))
	}
	if !*verbose {
		fmt.Printf("đã chuyển %d LUT sang %s\n", len(inputs), *outDir)
	}
}

func convert(inPath, outDir string) (string, error) {
	f, err := os.Open(inPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	cube, err := lut.ParseCube(f)
	if err != nil {
		return "", err
	}

	hald := cube.ToHald()
	if err := lut.ValidateHald(hald); err != nil {
		return "", err
	}

	base := strings.TrimSuffix(filepath.Base(inPath), filepath.Ext(inPath))
	outPath := filepath.Join(outDir, base+".png")

	// Ghi ra file tạm rồi đổi tên: nếu quá trình bị ngắt giữa chừng, sẽ không để
	// lại một file PNG cụt mà build vẫn coi là hợp lệ.
	tmp, err := os.CreateTemp(outDir, ".lutconv-*.png")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	// Nén tối đa: LUT được đóng gói vào app, dung lượng cài đặt đáng để đổi lấy
	// vài mili giây lúc build.
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(tmp, hald); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, outPath); err != nil {
		return "", err
	}
	return outPath, nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "lutconv: "+format+"\n", args...)
	os.Exit(1)
}
