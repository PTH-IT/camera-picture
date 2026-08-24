package api

import (
	"errors"
	"net/http"

	"github.com/hauph/camera/backend/internal/billing"
	"github.com/hauph/camera/backend/internal/protocol"
	"github.com/hauph/camera/backend/internal/storage"
	"github.com/hauph/camera/backend/internal/storage/gdrive"
)

// StorageDeps gom các phụ thuộc của tính năng lưu trữ và mua dung lượng.
//
// Cho phép nil từng phần: một bản triển khai chưa cấu hình MinIO hay Google
// vẫn phải khởi động được và trả 501 rõ ràng cho đúng những endpoint đó, thay vì
// sập lúc khởi động hoặc trả 500 khó hiểu lúc chạy.
type StorageDeps struct {
	Registry *storage.Registry
	Drive    *gdrive.Store
	Billing  *billing.Service
	// Selection lưu lựa chọn nhà cung cấp của từng người dùng.
	Selection SelectionStore
}

type SelectionStore interface {
	Selected(userID string) (storage.ProviderID, error)
	Select(userID string, p storage.ProviderID) error
}

func (s *Server) storageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/storage/options", s.requireAuth(s.storageOptions))
	mux.HandleFunc("GET /v1/storage/usage", s.requireAuth(s.storageUsage))
	mux.HandleFunc("POST /v1/storage/select", s.requireAuth(s.storageSelect))
	mux.HandleFunc("GET /v1/storage/drive/auth-url", s.requireAuth(s.driveAuthURL))
	mux.HandleFunc("POST /v1/storage/drive/link", s.requireAuth(s.driveLink))
	mux.HandleFunc("POST /v1/billing/redeem", s.requireAuth(s.redeemPurchase))
}

func (s *Server) notConfigured(w http.ResponseWriter, what string) {
	// 501 chứ không phải 500: đây không phải lỗi, mà là tính năng chưa được bật
	// trên bản triển khai này. Client phân biệt được để ẩn nút thay vì báo lỗi.
	fail(w, http.StatusNotImplemented, protocol.ErrCodeNotConfigured,
		what+" chưa được cấu hình trên máy chủ này")
}

func (s *Server) storageOptions(w http.ResponseWriter, r *http.Request) {
	if s.storage.Registry == nil {
		s.notConfigured(w, "lưu trữ")
		return
	}
	user, _ := userFrom(r.Context())

	selected := storage.ProviderID("")
	if s.storage.Selection != nil {
		selected, _ = s.storage.Selection.Selected(user.ID)
	}

	respond(w, http.StatusOK, map[string]any{
		"options":  s.storage.Registry.Options(),
		"selected": selected,
	})
}

func (s *Server) storageUsage(w http.ResponseWriter, r *http.Request) {
	if s.storage.Registry == nil || s.storage.Selection == nil {
		s.notConfigured(w, "lưu trữ")
		return
	}
	user, _ := userFrom(r.Context())

	id, err := s.storage.Selection.Selected(user.ID)
	if err != nil || id == "" {
		id = storage.ProviderDevice
	}
	// device không có gì để báo: ảnh nằm trên thẻ và điện thoại, server không
	// biết và không nên đoán.
	if id == storage.ProviderDevice {
		respond(w, http.StatusOK, storage.Usage{Provider: storage.ProviderDevice})
		return
	}

	p, err := s.storage.Registry.Get(id)
	if err != nil {
		s.notConfigured(w, string(id))
		return
	}
	usage, err := p.Usage(r.Context(), user.ID)
	if err != nil {
		s.failStorage(w, err)
		return
	}
	respond(w, http.StatusOK, usage)
}

type selectRequest struct {
	Provider storage.ProviderID `json:"provider"`
}

func (s *Server) storageSelect(w http.ResponseWriter, r *http.Request) {
	if s.storage.Registry == nil || s.storage.Selection == nil {
		s.notConfigured(w, "lưu trữ")
		return
	}
	var req selectRequest
	if !decode(w, r, &req) {
		return
	}
	user, _ := userFrom(r.Context())

	// device luôn hợp lệ và không cần provider nào đăng ký — nó nghĩa là "không
	// đồng bộ lên đâu cả".
	if req.Provider != storage.ProviderDevice {
		if _, err := s.storage.Registry.Get(req.Provider); err != nil {
			fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput,
				"nhà cung cấp không khả dụng")
			return
		}
	}

	if err := s.storage.Selection.Select(user.ID, req.Provider); err != nil {
		s.log.Error("lưu lựa chọn nhà cung cấp", "err", err)
		fail(w, http.StatusInternalServerError, protocol.ErrCodeInternal, "lỗi máy chủ")
		return
	}

	// Đổi nhà cung cấp KHÔNG di chuyển dữ liệu cũ. Nói rõ trong phản hồi để giao
	// diện hiển thị được, thay vì để người dùng phát hiện khi ảnh cũ biến mất.
	respond(w, http.StatusOK, map[string]any{
		"selected": req.Provider,
		"note":     "Ảnh đã lưu ở nhà cung cấp trước vẫn ở nguyên đó và không tự động chuyển sang.",
	})
}

func (s *Server) driveAuthURL(w http.ResponseWriter, r *http.Request) {
	if s.storage.Drive == nil {
		s.notConfigured(w, "Google Drive")
		return
	}
	user, _ := userFrom(r.Context())

	// state gắn với người dùng để chống CSRF ở luồng OAuth: không có nó, kẻ tấn
	// công dụ nạn nhân hoàn tất luồng uỷ quyền bằng mã của CHÍNH kẻ tấn công, và
	// ảnh của nạn nhân sẽ đổ vào Drive của hắn.
	respond(w, http.StatusOK, map[string]string{
		"url": s.storage.Drive.AuthURL(user.ID),
	})
}

type driveLinkRequest struct {
	Code string `json:"code"`
}

func (s *Server) driveLink(w http.ResponseWriter, r *http.Request) {
	if s.storage.Drive == nil {
		s.notConfigured(w, "Google Drive")
		return
	}
	var req driveLinkRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Code == "" {
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "thiếu code")
		return
	}
	user, _ := userFrom(r.Context())

	if err := s.storage.Drive.Link(r.Context(), user.ID, req.Code); err != nil {
		s.failStorage(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type redeemRequest struct {
	Platform billing.Platform `json:"platform"`
	Receipt  string           `json:"receipt"`
}

func (s *Server) redeemPurchase(w http.ResponseWriter, r *http.Request) {
	if s.storage.Billing == nil {
		s.notConfigured(w, "mua dung lượng")
		return
	}
	var req redeemRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Platform != billing.PlatformApple && req.Platform != billing.PlatformGoogle {
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "platform phải là apple hoặc google")
		return
	}
	if req.Receipt == "" {
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "thiếu receipt")
		return
	}
	user, _ := userFrom(r.Context())

	// userID lấy từ TOKEN, không phải từ body. Cho client tự khai userID nghĩa là
	// bất kỳ ai cũng gán quyền lợi cho tài khoản bất kỳ.
	ent, err := s.storage.Billing.Redeem(r.Context(), user.ID, req.Platform, req.Receipt)
	if err != nil {
		s.failBilling(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"productId":    ent.ProductID,
		"storageBytes": ent.StorageBytes,
		"expiresAt":    ent.ExpiresAt,
	})
}

func (s *Server) failStorage(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrQuotaExceeded):
		fail(w, http.StatusInsufficientStorage, protocol.ErrCodeQuotaExceeded, err.Error())
	case errors.Is(err, storage.ErrNotLinked), errors.Is(err, gdrive.ErrNoRefreshToken):
		// Mã riêng để giao diện hiển thị đúng hành động: "liên kết lại Drive",
		// chứ không phải một thông báo lỗi chung mà người dùng không làm gì được.
		fail(w, http.StatusConflict, protocol.ErrCodeNotLinked,
			"cần liên kết lại tài khoản lưu trữ")
	case errors.Is(err, storage.ErrProviderNotFound):
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "nhà cung cấp không khả dụng")
	default:
		s.log.Error("lỗi lưu trữ", "err", err)
		fail(w, http.StatusInternalServerError, protocol.ErrCodeInternal, "lỗi máy chủ")
	}
}

func (s *Server) failBilling(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, billing.ErrReceiptInvalid):
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "hoá đơn không hợp lệ")
	case errors.Is(err, billing.ErrUnknownProduct):
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "mã sản phẩm không xác định")
	case errors.Is(err, billing.ErrAlreadyClaimed):
		fail(w, http.StatusConflict, protocol.ErrCodeConflict,
			"giao dịch này đã thuộc về một tài khoản khác")
	default:
		s.log.Error("lỗi thanh toán", "err", err)
		fail(w, http.StatusInternalServerError, protocol.ErrCodeInternal, "lỗi máy chủ")
	}
}
