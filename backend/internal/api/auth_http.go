package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/hauph/camera/backend/internal/auth"
	"github.com/hauph/camera/backend/internal/protocol"
)

type ctxKey int

const ctxKeyUser ctxKey = iota

// userFrom lấy người dùng đã xác thực từ context.
//
// Trả về ok = false thay vì panic: handler nào quên bọc middleware sẽ trả 401 chứ
// không làm sập tiến trình. Nhưng đó vẫn là lỗi lập trình, nên
// TestEveryProtectedRouteRequiresAuth tồn tại để bắt trước khi lên production.
func userFrom(ctx context.Context) (auth.User, bool) {
	u, ok := ctx.Value(ctxKeyUser).(auth.User)
	return u, ok
}

// requireAuth là middleware xác thực Bearer token.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			fail(w, http.StatusUnauthorized, protocol.ErrCodeUnauthorized, "thiếu token")
			return
		}
		user, err := s.auth.Authenticate(r.Context(), token)
		if err != nil {
			// Luôn trả lỗi chung. Phân biệt "token sai định dạng" với "token hết
			// hạn" cho kẻ tấn công biết token nào từng hợp lệ.
			fail(w, http.StatusUnauthorized, protocol.ErrCodeUnauthorized, "phiên không hợp lệ")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKeyUser, user)))
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	// So khớp không phân biệt hoa thường: một số HTTP client gửi "bearer".
	if len(h) < 7 || !strings.EqualFold(h[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

type signUpRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name,omitempty"`
}

type signInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type oidcRequest struct {
	Provider string `json:"provider"`
	IDToken  string `json:"idToken"`
	Nonce    string `json:"nonce"`
	// Name chỉ được gửi ở lần uỷ quyền ĐẦU TIÊN với Apple. Apple không trả tên
	// trong ID token và không bao giờ trả lại lần thứ hai, nên client phải chuyển
	// tiếp ở lần đầu và server lưu ngay.
	Name string `json:"name,omitempty"`
}

type authResponse struct {
	Token string      `json:"token"`
	User  userPayload `json:"user"`
}

type userPayload struct {
	ID            string `json:"id"`
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"emailVerified"`
	Name          string `json:"name,omitempty"`
}

func toPayload(u auth.User) userPayload {
	return userPayload{ID: u.ID, Email: u.Email, EmailVerified: u.EmailVerified, Name: u.Name}
}

func (s *Server) signUp(w http.ResponseWriter, r *http.Request) {
	var req signUpRequest
	if !decode(w, r, &req) {
		return
	}
	token, user, err := s.auth.SignUpWithPassword(r.Context(), req.Email, req.Password, req.Name)
	if err != nil {
		s.failAuth(w, err)
		return
	}
	respond(w, http.StatusCreated, authResponse{Token: token, User: toPayload(user)})
}

func (s *Server) signIn(w http.ResponseWriter, r *http.Request) {
	var req signInRequest
	if !decode(w, r, &req) {
		return
	}
	token, user, err := s.auth.SignInWithPassword(r.Context(), req.Email, req.Password)
	if err != nil {
		s.failAuth(w, err)
		return
	}
	respond(w, http.StatusOK, authResponse{Token: token, User: toPayload(user)})
}

func (s *Server) signInOIDC(w http.ResponseWriter, r *http.Request) {
	var req oidcRequest
	if !decode(w, r, &req) {
		return
	}

	var p auth.Provider
	switch req.Provider {
	case string(auth.ProviderApple):
		p = auth.ProviderApple
	case string(auth.ProviderGoogle):
		p = auth.ProviderGoogle
	default:
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "provider phải là apple hoặc google")
		return
	}
	if req.IDToken == "" || req.Nonce == "" {
		// Nonce bắt buộc: không có nó, một ID token hợp lệ bị chặn được có thể
		// phát lại để đăng nhập dưới danh nghĩa nạn nhân.
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, "cần idToken và nonce")
		return
	}

	token, user, err := s.auth.SignInWithOIDC(r.Context(), p, req.IDToken, req.Nonce, req.Name)
	if err != nil {
		s.failAuth(w, err)
		return
	}
	respond(w, http.StatusOK, authResponse{Token: token, User: toPayload(user)})
}

func (s *Server) signOut(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.SignOut(r.Context(), bearerToken(r)); err != nil {
		s.log.Error("đăng xuất", "err", err)
	}
	// Luôn trả 204: đăng xuất một token đã hết hạn không phải lỗi của người dùng,
	// và họ chẳng làm gì được với thông báo lỗi đó.
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) signOutEverywhere(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		fail(w, http.StatusUnauthorized, protocol.ErrCodeUnauthorized, "phiên không hợp lệ")
		return
	}
	if err := s.auth.SignOutEverywhere(r.Context(), user.ID); err != nil {
		s.log.Error("đăng xuất mọi nơi", "err", err)
		fail(w, http.StatusInternalServerError, protocol.ErrCodeInternal, "lỗi máy chủ")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		fail(w, http.StatusUnauthorized, protocol.ErrCodeUnauthorized, "phiên không hợp lệ")
		return
	}
	respond(w, http.StatusOK, toPayload(user))
}

// failAuth ánh xạ lỗi tầng auth sang mã HTTP.
//
// Mọi lỗi thông tin đăng nhập đều thành cùng một 401 với cùng một thông báo:
// phân biệt "email chưa đăng ký" với "sai mật khẩu" biến form đăng nhập thành
// công cụ dò xem ai đã dùng dịch vụ.
func (s *Server) failAuth(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrWrongCredentials):
		fail(w, http.StatusUnauthorized, protocol.ErrCodeUnauthorized, "email hoặc mật khẩu không đúng")
	case errors.Is(err, auth.ErrEmailTaken):
		fail(w, http.StatusConflict, protocol.ErrCodeConflict, "email đã được dùng")
	case errors.Is(err, auth.ErrWeakPassword):
		fail(w, http.StatusBadRequest, protocol.ErrCodeInvalidInput, err.Error())
	case errors.Is(err, auth.ErrLinkRequiresSignIn):
		// 409 kèm mã riêng để client hiển thị được hướng dẫn đúng: "email này đã
		// có tài khoản, hãy đăng nhập bằng cách cũ rồi liên kết".
		fail(w, http.StatusConflict, protocol.ErrCodeLinkRequired,
			"email đã tồn tại với phương thức đăng nhập khác")
	case errors.Is(err, auth.ErrInvalidToken),
		errors.Is(err, auth.ErrTokenExpired),
		errors.Is(err, auth.ErrNonceMismatch):
		fail(w, http.StatusUnauthorized, protocol.ErrCodeUnauthorized, "token không hợp lệ")
	default:
		s.log.Error("lỗi xác thực", "err", err)
		fail(w, http.StatusInternalServerError, protocol.ErrCodeInternal, "lỗi máy chủ")
	}
}
