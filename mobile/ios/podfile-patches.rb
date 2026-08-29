# Vá các pod của bên thứ ba sau khi CocoaPods cài xong.
#
# Tách ra file riêng thay vì nhét vào Podfile: Podfile do template React Native
# sinh ra và `bootstrap.sh` chép về, nên mọi thứ viết thẳng trong đó sẽ biến mất
# ở lần sinh lại tiếp theo. Ở đây thì nó nằm trong repo, review được, và Podfile
# chỉ cần một dòng gọi.

# fmt 11.0.2 không biên dịch được bằng Clang của Xcode 26.
#
# Lỗi: "call to consteval function 'fmt::basic_format_string<...>' is not a
# constant expression", hàng chục lần, trong chính header của fmt. Trình biên
# dịch mới siết chặt consteval và từ chối luôn những lời gọi nội bộ của fmt.
# Upstream sửa ở 11.1, nhưng React Native 0.81 ghim `~> 11.0.2` qua nhiều
# podspec — nâng phiên bản là phá ràng buộc của chúng.
#
# Tắt consteval trong fmt là đủ và không đổi hành vi lúc chạy: nó chỉ chuyển
# việc kiểm chuỗi định dạng từ lúc biên dịch sang lúc chạy. React Native không
# dùng fmt cho chuỗi do người dùng nhập, nên mất kiểm tra lúc biên dịch ở đây
# không tạo ra rủi ro nào.
#
# Phải sửa THẲNG header: khối dò tìm trong base.h `#define FMT_USE_CONSTEVAL`
# vô điều kiện, nên truyền `-DFMT_USE_CONSTEVAL=0` từ dòng lệnh sẽ bị đè.
def fix_fmt_consteval(installer)
  path = File.join(installer.sandbox.root, 'fmt', 'include', 'fmt', 'base.h')
  return unless File.exist?(path)

  source = File.read(path)
  marker = "#if !defined(__cpp_lib_is_constant_evaluated)\n#  define FMT_USE_CONSTEVAL 0"

  unless source.include?(marker)
    # Đã vá rồi, hoặc fmt đã lên phiên bản khác. Im lặng bỏ qua là sai: nếu fmt
    # đổi mà bản vá không còn áp được, người build cần biết trước khi ngồi đọc
    # một trang lỗi consteval.
    Pod::UI.puts '[camera-picture] fmt: không tìm thấy khối consteval — bỏ qua ' \
                 '(đã vá, hoặc fmt đã đổi phiên bản: kiểm tra lại podfile-patches.rb)'
    return
  end

  patched = source.sub(
    marker,
    "#if 1  // camera-picture: tắt consteval cho Clang của Xcode 26\n#  define FMT_USE_CONSTEVAL 0",
  )

  # CocoaPods chép mã nguồn pod về ở chế độ CHỈ ĐỌC, nên ghi thẳng sẽ nhận
  # "Permission denied" — và vì chuyện đó xảy ra trong post_install hook, thông
  # báo lỗi nói về Ruby chứ không nói gì tới quyền file.
  File.chmod(0o644, path)
  File.write(path, patched)
  Pod::UI.puts '[camera-picture] fmt: đã tắt consteval'
end
