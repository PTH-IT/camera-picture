#!/usr/bin/env bash
#
# Sinh bộ icon cho iOS từ icon.svg.
#
# Dùng Chrome headless để dựng SVG rồi `sips` để đổ ra các cỡ. Không kéo thư viện
# nào về: cả hai đều có sẵn trên máy đã cài Chrome và macOS, và một bộ icon được
# sinh lại vài lần trong đời dự án — không đáng để thêm một phụ thuộc.
#
# Chạy:
#   cd mobile/assets/icon && ./make-icons.sh
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="$DIR/../../ios/CameraPicture/Images.xcassets/AppIcon.appiconset"

CHROME="${CHROME_PATH:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
[ -x "$CHROME" ] || { echo "Không thấy Chrome ở $CHROME — đặt CHROME_PATH." >&2; exit 1; }
[ -d "$OUT" ] || { echo "Không thấy thư mục AppIcon.appiconset ở $OUT." >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Bọc SVG trong HTML không lề: chụp thẳng file .svg sẽ dính lề mặc định của
# trình duyệt và icon bị lệch tâm vài pixel.
cat > "$TMP/icon.html" <<HTML
<!doctype html><meta charset="utf-8">
<style>html,body{margin:0;padding:0;background:#131316}svg{display:block}</style>
$(cat "$DIR/icon.svg")
HTML

"$CHROME" --headless --disable-gpu --hide-scrollbars \
  --screenshot="$TMP/icon-1024.png" --window-size=1024,1024 \
  "file://$TMP/icon.html" >/dev/null 2>&1

[ -s "$TMP/icon-1024.png" ] || { echo "Chrome không dựng được ảnh." >&2; exit 1; }

# Bộ cỡ của asset catalog. Tên file phải khớp Contents.json bên cạnh.
for spec in "40:icon-20@2x.png" "60:icon-20@3x.png" \
            "58:icon-29@2x.png" "87:icon-29@3x.png" \
            "80:icon-40@2x.png" "120:icon-40@3x.png" \
            "120:icon-60@2x.png" "180:icon-60@3x.png" \
            "1024:icon-1024.png"; do
  px="${spec%%:*}"
  name="${spec##*:}"
  sips -s format png -z "$px" "$px" "$TMP/icon-1024.png" --out "$OUT/$name" >/dev/null
done

echo "Đã sinh $(ls "$OUT"/*.png | wc -l | tr -d ' ') file icon vào $OUT"
