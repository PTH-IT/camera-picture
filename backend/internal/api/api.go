// Package api là tầng HTTP của backend.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/hauph/camera/backend/internal/auth"
	"github.com/hauph/camera/backend/internal/protocol"
	"github.com/hauph/camera/backend/internal/store"
)

// maxBodyBytes chặn body quá lớn.
//
// 8MB đủ cho khoảng 20 nghìn bản ghi metadata — thừa cho mọi lô hợp lệ, vì client
// nên chia lô vài trăm ảnh. Không có giới hạn thì một client lỗi có thể làm cạn
// bộ nhớ server chỉ bằng một request.
const maxBodyBytes = 8 << 20

type Server struct {
	store   store.Store
	auth    *auth.Service
	storage StorageDeps
	log     *slog.Logger
}

func New(s store.Store, a *auth.Service, sd StorageDeps, log *slog.Logger) *Server {
	return &Server{store: s, auth: a, storage: sd, log: log}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Đăng nhập/đăng ký: công khai theo bản chất.
	mux.HandleFunc("POST /v1/auth/signup", s.signUp)
	mux.HandleFunc("POST /v1/auth/signin", s.signIn)
	mux.HandleFunc("POST /v1/auth/oidc", s.signInOIDC)
	mux.HandleFunc("POST /v1/auth/signout", s.signOut)

	mux.HandleFunc("POST /v1/auth/signout-everywhere", s.requireAuth(s.signOutEverywhere))
	mux.HandleFunc("GET /v1/me", s.requireAuth(s.me))

	// Mọi route dữ liệu đều phải xác thực. Thiếu requireAuth ở một dòng là lộ dữ
	// liệu người dùng — TestEveryProtectedRouteRequiresAuth kiểm tra từng route.
	mux.HandleFunc("POST /v1/sessions", s.requireAuth(s.createSession))
	mux.HandleFunc("POST /v1/sessions/{sessionID}/images/batch", s.requireAuth(s.batchImages))
	mux.HandleFunc("GET /v1/sessions/{sessionID}/changes", s.requireAuth(s.changes))
	mux.HandleFunc("PUT /v1/images/{imageID}/edit", s.requireAuth(s.putEdit))
	mux.HandleFunc("POST /v1/images/{imageID}/assets/confirm", s.requireAuth(s.confirmAsset))
	mux.HandleFunc("DELETE /v1/images/{imageID}", s.requireAuth(s.deleteImage))

	s.storageRoutes(mux)

	return mux
}

type createSessionRequest struct {
	Name       string    `json:"name"`
	ClientName string    `json:"clientName,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "name không được rỗng")
		return
	}
	if req.StartedAt.IsZero() {
		req.StartedAt = time.Now()
	}

	user, ok := userFrom(r.Context())
	if !ok {
		fail(w, http.StatusUnauthorized, protocol.ErrCodeUnauthorized, "phiên không hợp lệ")
		return
	}

	sess, err := s.store.CreateSession(r.Context(), user.ID, req.Name, req.ClientName, req.StartedAt)
	if err != nil {
		s.failStore(w, err, "CreateSession")
		return
	}
	respond(w, http.StatusCreated, sess)
}

func (s *Server) batchImages(w http.ResponseWriter, r *http.Request) {
	if !s.ownsSession(w, r, r.PathValue("sessionID")) {
		return
	}

	var req protocol.BatchImagesRequest
	if !decode(w, r, &req) {
		return
	}
	if len(req.Images) == 0 {
		// Lô rỗng là hợp lệ, không phải lỗi: client retry mù có thể gửi lô rỗng
		// sau khi đã lọc hết. Trả về trạng thái hiện tại để nó cập nhật con trỏ.
		sess, err := s.store.GetSession(r.Context(), r.PathValue("sessionID"))
		if err != nil {
			s.failStore(w, err, "GetSession")
			return
		}
		respond(w, http.StatusOK, protocol.BatchImagesResponse{
			IDs: map[string]string{}, Revision: sess.Revision,
		})
		return
	}

	res, err := s.store.BatchUpsertImages(r.Context(), r.PathValue("sessionID"), req.Images)
	if err != nil {
		s.failStore(w, err, "BatchUpsertImages")
		return
	}
	respond(w, http.StatusOK, protocol.BatchImagesResponse{
		IDs:      res.IDs,
		Created:  res.Created,
		Updated:  res.Updated,
		Revision: res.Revision,
	})
}

func (s *Server) changes(w http.ResponseWriter, r *http.Request) {
	if !s.ownsSession(w, r, r.PathValue("sessionID")) {
		return
	}

	q := r.URL.Query()

	since, err := parseInt64(q.Get("since"), 0)
	if err != nil {
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "since phải là số nguyên")
		return
	}
	limit, err := parseInt64(q.Get("limit"), 0)
	if err != nil {
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "limit phải là số nguyên")
		return
	}

	resp, err := s.store.Changes(r.Context(), r.PathValue("sessionID"), since, int(limit))
	if err != nil {
		s.failStore(w, err, "Changes")
		return
	}
	// Đảm bảo mảng rỗng serialize thành [] chứ không phải null: client
	// TypeScript sẽ vấp phải null nếu không.
	if resp.Images == nil {
		resp.Images = []protocol.ImageRecord{}
	}
	if resp.Edits == nil {
		resp.Edits = []protocol.EditRecord{}
	}
	respond(w, http.StatusOK, resp)
}

func (s *Server) putEdit(w http.ResponseWriter, r *http.Request) {
	if !s.ownsImage(w, r, r.PathValue("imageID")) {
		return
	}

	var req protocol.PutEditRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Rating < 0 || req.Rating > 5 {
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "rating phải trong khoảng 0..5")
		return
	}

	rec, err := s.store.PutEdit(r.Context(), r.PathValue("imageID"), req)
	if err != nil {
		s.failStore(w, err, "PutEdit")
		return
	}
	respond(w, http.StatusOK, rec)
}

func (s *Server) confirmAsset(w http.ResponseWriter, r *http.Request) {
	if !s.ownsImage(w, r, r.PathValue("imageID")) {
		return
	}

	var req protocol.ConfirmAssetRequest
	if !decode(w, r, &req) {
		return
	}
	if !validTier(req.Tier) {
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "tier không hợp lệ")
		return
	}
	if req.StorageKey == "" {
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "storageKey không được rỗng")
		return
	}

	if err := s.store.ConfirmAsset(r.Context(), r.PathValue("imageID"), req); err != nil {
		s.failStore(w, err, "ConfirmAsset")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteImage(w http.ResponseWriter, r *http.Request) {
	if !s.ownsImage(w, r, r.PathValue("imageID")) {
		return
	}

	if err := s.store.SoftDeleteImage(r.Context(), r.PathValue("imageID")); err != nil {
		s.failStore(w, err, "SoftDeleteImage")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ownsSession kiểm tra buổi chụp thuộc về người dùng đang gọi.
//
// Trả về 404 chứ KHÔNG phải 403 khi không sở hữu. Trả 403 là xác nhận "id này có
// tồn tại, chỉ là không phải của bạn" — đủ để kẻ tấn công dò ra id hợp lệ. Với
// tài nguyên riêng tư, không tồn tại và không thuộc về bạn phải trông giống nhau.
func (s *Server) ownsSession(w http.ResponseWriter, r *http.Request, sessionID string) bool {
	user, ok := userFrom(r.Context())
	if !ok {
		fail(w, http.StatusUnauthorized, protocol.ErrCodeUnauthorized, "phiên không hợp lệ")
		return false
	}
	sess, err := s.store.GetSession(r.Context(), sessionID)
	if err != nil || sess.UserID != user.ID {
		fail(w, http.StatusNotFound, protocol.ErrCodeNotFound, "không tìm thấy")
		return false
	}
	return true
}

// ownsImage kiểm tra ảnh thuộc buổi chụp của người dùng đang gọi.
func (s *Server) ownsImage(w http.ResponseWriter, r *http.Request, imageID string) bool {
	sessionID, err := s.store.SessionOfImage(r.Context(), imageID)
	if err != nil {
		fail(w, http.StatusNotFound, protocol.ErrCodeNotFound, "không tìm thấy")
		return false
	}
	return s.ownsSession(w, r, sessionID)
}

func validTier(t protocol.AssetTier) bool {
	switch t {
	case protocol.TierThumb, protocol.TierPreview, protocol.TierProxy,
		protocol.TierOriginal, protocol.TierExport:
		return true
	}
	return false
}

func parseInt64(s string, def int64) (int64, error) {
	if s == "" {
		return def, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	// Từ chối trường lạ thay vì bỏ qua âm thầm: client gửi `clientID` thay vì
	// `clientId` mà không báo lỗi sẽ tạo ra ảnh trùng lặp hàng loạt, và triệu
	// chứng xuất hiện rất xa nguyên nhân.
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, err.Error())
		return false
	}
	return true
}

func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	respond(w, status, protocol.ErrorResponse{Code: code, Message: msg})
}

// failStore ánh xạ lỗi tầng store sang mã HTTP.
//
// Lỗi không nhận diện được KHÔNG bao giờ lộ nội dung ra client — thông báo lỗi
// của database hay lộ tên bảng và cấu trúc truy vấn. Ghi chi tiết vào log, trả
// về thông báo chung.
func (s *Server) failStore(w http.ResponseWriter, err error, op string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, protocol.ErrCodeNotFound, "không tìm thấy")
	case errors.Is(err, store.ErrConflict):
		fail(w, http.StatusConflict, protocol.ErrCodeConflict, err.Error())
	default:
		s.log.Error("lỗi store", "op", op, "err", err)
		fail(w, http.StatusInternalServerError, protocol.ErrCodeInternal, "lỗi máy chủ")
	}
}
