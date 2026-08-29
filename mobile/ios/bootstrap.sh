#!/usr/bin/env bash
#
# Sinh dự án Xcode cho ứng dụng, rồi nối tầng capture vào.
#
# Dự án Xcode chưa có trong repo vì nó không sinh ra được trên máy không có
# Xcode, mà máy phát triển chính của dự án là Windows. Script này dựng nó bằng
# một lệnh trên máy Mac, thay vì một danh sách thao tác tay trong tài liệu mà
# không ai kiểm chứng được là đã làm đúng.
#
# CHẠY XONG THÌ COMMIT dự án vừa sinh. Nó không nằm trong .gitignore, và đó là
# chủ ý: mọi thay đổi làm bằng tay trong Xcode — thêm CascableCore qua Swift
# Package Manager, cấu hình ký tên, capability — đều nằm trong `project.pbxproj`.
# Sinh lại từ đầu mỗi lần là mất sạch những thứ đó.
#
# Chạy:
#   cd mobile/ios && ./bootstrap.sh
#
# Sau khi chạy xong:
#   cd mobile && npm start          # Metro
#   cd mobile && npm run ios        # build và mở simulator
#
# Chạy lại được nhiều lần: nếu dự án đã tồn tại thì script chỉ cài lại pod.
set -euo pipefail

RN_VERSION='0.81.4'
APP_NAME='CameraPicture'

IOS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MOBILE_DIR="$(dirname "$IOS_DIR")"

say()  { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
die()  { printf '\n\033[31mLỗi: %s\033[0m\n' "$1" >&2; exit 1; }

# --- Điều kiện cần -----------------------------------------------------------
#
# Kiểm hết một lượt rồi mới báo, thay vì dừng ở cái thiếu đầu tiên: người phải
# đi cài Xcode 10GB muốn biết luôn là còn thiếu gì nữa, không phải chạy lại
# script ba lần để phát hiện từng cái một.

missing=()
command -v node >/dev/null      || missing+=('node (Node 20+)')
command -v npx  >/dev/null      || missing+=('npx')
command -v xcodebuild >/dev/null || missing+=('Xcode đầy đủ — Command Line Tools KHÔNG đủ')
xcrun --find simctl >/dev/null 2>&1 || missing+=('simctl (đi kèm Xcode)')

if [ ${#missing[@]} -gt 0 ]; then
  printf '\n\033[31mThiếu công cụ:\033[0m\n'
  printf '  - %s\n' "${missing[@]}"
  cat <<'HELP'

Xcode cài từ App Store, rồi trỏ dòng lệnh vào nó:

  sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
  sudo xcodebuild -license accept
  xcodebuild -runFirstLaunch

`xcode-select -p` phải trả về đường dẫn trong Xcode.app, không phải
/Library/Developer/CommandLineTools.
HELP
  exit 1
fi

# --- Sinh dự án --------------------------------------------------------------

if [ -d "$IOS_DIR/$APP_NAME.xcodeproj" ]; then
  say "Dự án Xcode đã có, bỏ qua bước sinh"
else
  say "Sinh dự án React Native $RN_VERSION vào thư mục tạm"

  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "$TMP_DIR"' EXIT

  # Sinh ra chỗ khác rồi mới chép phần `ios/` vào: chạy thẳng vào `mobile/` sẽ
  # ghi đè package.json, App.tsx, metro.config.js — tức là xoá mất toàn bộ mã
  # nguồn thật của dự án bằng bản mẫu rỗng.
  npx --yes @react-native-community/cli@latest init "$APP_NAME" \
    --version "$RN_VERSION" \
    --directory "$TMP_DIR/$APP_NAME" \
    --skip-install \
    --skip-git-init

  [ -d "$TMP_DIR/$APP_NAME/ios" ] || die "template không sinh ra thư mục ios/"

  say "Chép dự án Xcode vào mobile/ios (giữ nguyên CaptureSource/)"
  # `cp -R src/. dest/` chép NỘI DUNG chứ không lồng thêm một cấp thư mục, và
  # không đụng tới những gì đã có sẵn ngoài các file trùng tên.
  cp -R "$TMP_DIR/$APP_NAME/ios/." "$IOS_DIR/"

  # Gemfile ghim phiên bản CocoaPods. Thiếu nó thì mỗi máy dùng một bản pod khác
  # nhau, và Podfile.lock sẽ nhảy qua nhảy lại giữa các lần commit.
  if [ -f "$TMP_DIR/$APP_NAME/Gemfile" ] && [ ! -f "$MOBILE_DIR/Gemfile" ]; then
    cp "$TMP_DIR/$APP_NAME/Gemfile" "$MOBILE_DIR/Gemfile"
  fi
fi

# --- Nối native module vào Podfile ------------------------------------------
#
# `use_native_modules!` chỉ autolink những gì nằm trong node_modules. CaptureSource
# nằm trong repo nên phải khai tường minh.

PODFILE="$IOS_DIR/Podfile"
[ -f "$PODFILE" ] || die "không thấy Podfile ở $PODFILE"

if grep -q "pod 'CaptureSource'" "$PODFILE"; then
  say "Podfile đã khai CaptureSource"
else
  say "Thêm CaptureSource vào Podfile"
  awk '
    { print }
    /use_native_modules!/ && !done {
      print "";
      print "  # Tầng capture: native module nằm trong repo, không phải trong";
      print "  # node_modules, nên autolink không thấy. Xem CaptureSource/CaptureSource.podspec.";
      print "  pod '\''CaptureSource'\'', :path => '\''./CaptureSource'\''";
      done = 1;
    }
  ' "$PODFILE" > "$PODFILE.tmp" && mv "$PODFILE.tmp" "$PODFILE"

  grep -q "pod 'CaptureSource'" "$PODFILE" || die "không chèn được vào Podfile — sửa tay: pod 'CaptureSource', :path => './CaptureSource'"
fi

# --- Quyền trong Info.plist --------------------------------------------------
#
# Thiếu NSLocalNetworkUsageDescription hoặc NSBonjourServices thì việc tìm máy
# ảnh qua Wi-Fi im lặng không trả kết quả nào — không lỗi, không cảnh báo, chỉ
# là danh sách rỗng mãi mãi. Đây là một trong những lỗi tốn thời gian nhất khi
# làm tether trên iOS, nên nó được đặt vào bằng script chứ không bằng trí nhớ.

PLIST="$IOS_DIR/$APP_NAME/Info.plist"
[ -f "$PLIST" ] || die "không thấy Info.plist ở $PLIST"

plist_set_string() {
  /usr/libexec/PlistBuddy -c "Delete :$1" "$PLIST" >/dev/null 2>&1 || true
  /usr/libexec/PlistBuddy -c "Add :$1 string $2" "$PLIST"
}

say "Khai quyền máy ảnh và mạng nội bộ vào Info.plist"
plist_set_string NSCameraUsageDescription 'Kết nối với máy ảnh của bạn để nhận ảnh trong lúc chụp.'
plist_set_string NSLocalNetworkUsageDescription 'Kết nối Wi-Fi trực tiếp tới máy ảnh của bạn.'

/usr/libexec/PlistBuddy -c 'Delete :NSBonjourServices' "$PLIST" >/dev/null 2>&1 || true
/usr/libexec/PlistBuddy -c 'Add :NSBonjourServices array' "$PLIST"
/usr/libexec/PlistBuddy -c 'Add :NSBonjourServices:0 string _ptp._tcp' "$PLIST"

# --- Phụ thuộc ---------------------------------------------------------------

if [ ! -d "$MOBILE_DIR/node_modules" ]; then
  say 'Cài phụ thuộc JavaScript'
  (cd "$MOBILE_DIR" && npm install)
fi

say 'Cài CocoaPods'
if command -v pod >/dev/null; then
  (cd "$IOS_DIR" && pod install)
elif command -v bundle >/dev/null && [ -f "$MOBILE_DIR/Gemfile" ]; then
  (cd "$MOBILE_DIR" && bundle install)
  (cd "$IOS_DIR" && bundle exec pod install)
else
  die 'không có `pod` lẫn `bundle`. Cài: sudo gem install cocoapods'
fi

cat <<DONE

Xong. Dự án nằm ở mobile/ios/$APP_NAME.xcworkspace (mở workspace, KHÔNG mở
xcodeproj — pod chỉ được liên kết trong workspace).

Chạy thử:
  cd $MOBILE_DIR && npm start
  cd $MOBILE_DIR && npm run ios

Lần chạy đầu dùng MockBackend: máy ảnh giả bắn ảnh về mỗi 3,5 giây, đủ để thấy
toàn bộ luồng tether chạy thật mà không cần máy ảnh. Đổi sang CascableBackend
sau khi có license — xem CaptureSource/README.md.
DONE
