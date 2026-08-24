// Package migrations nhúng các file SQL vào binary.
//
// Nhúng thay vì đọc từ đĩa lúc chạy: binary triển khai đi một mình, không cần
// kèm thư mục migrations. Thiếu file lúc chạy là kiểu lỗi chỉ lộ ra trên
// production, khi đã quá muộn.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
