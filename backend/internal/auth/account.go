package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserNotFound       = errors.New("không tìm thấy người dùng")
	ErrWrongCredentials   = errors.New("email hoặc mật khẩu không đúng")
	ErrEmailTaken         = errors.New("email đã được dùng")
	ErrWeakPassword       = errors.New("mật khẩu quá yếu")
	ErrSessionInvalid     = errors.New("phiên không hợp lệ")
	ErrLinkRequiresSignIn = errors.New("email đã tồn tại với phương thức đăng nhập khác")
)

type User struct {
	ID            string
	Email         string
	EmailVerified bool
	Name          string
	CreatedAt     time.Time
}

type IdentityRecord struct {
	UserID   string
	Provider Provider
	Subject  string
	Email    string
}

type SessionRecord struct {
	UserID    string
	ExpiresAt time.Time
}

// Repo là tầng lưu trữ mà Service cần. Tách interface để test được toàn bộ logic
// tài khoản — vốn là nơi tập trung các quyết định bảo mật — mà không cần Postgres.
type Repo interface {
	UserByIdentity(ctx context.Context, p Provider, subject string) (User, error)
	UserByEmail(ctx context.Context, email string) (User, error)
	UserByID(ctx context.Context, id string) (User, error)
	CreateUser(ctx context.Context, email, name string, emailVerified bool) (User, error)
	LinkIdentity(ctx context.Context, rec IdentityRecord) error
	IdentitiesOf(ctx context.Context, userID string) ([]IdentityRecord, error)

	SetPasswordHash(ctx context.Context, userID string, hash []byte) error
	PasswordHash(ctx context.Context, userID string) ([]byte, error)

	CreateSession(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time) error
	SessionByTokenHash(ctx context.Context, tokenHash []byte) (SessionRecord, error)
	DeleteSession(ctx context.Context, tokenHash []byte) error
	DeleteSessionsOfUser(ctx context.Context, userID string) error
}

type Service struct {
	repo      Repo
	verifiers map[Provider]*Verifier
	now       func() time.Time

	// sessionTTL đủ dài để nhiếp ảnh gia không phải đăng nhập lại giữa buổi chụp.
	sessionTTL time.Duration
}

func NewService(repo Repo, verifiers map[Provider]*Verifier, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		repo:       repo,
		verifiers:  verifiers,
		now:        now,
		sessionTTL: 90 * 24 * time.Hour,
	}
}

// bcryptCost 12 là cân bằng giữa chi phí bẻ khoá và độ trễ đăng nhập (~250ms trên
// phần cứng hiện đại). Đừng hạ xuống để "đăng nhập nhanh hơn" — chi phí đó chính
// là thứ bảo vệ mật khẩu khi cơ sở dữ liệu bị lộ.
const bcryptCost = 12

// dummyHash dùng cho phòng thủ chống dò tài khoản qua thời gian phản hồi. Xem
// SignInWithPassword.
var dummyHash []byte

func init() {
	dummyHash, _ = bcrypt.GenerateFromPassword([]byte("chuoi-gia-de-can-bang-thoi-gian"), bcryptCost)
}

// SignInWithOIDC đăng nhập bằng Apple hoặc Google.
//
// firstAuthName là cạm bẫy riêng của Apple: Apple CHỈ trả tên người dùng ở lần
// uỷ quyền ĐẦU TIÊN, và chỉ trả cho client, không nằm trong ID token. Các lần sau
// chỉ có sub. Không lưu ngay lần đầu là mất vĩnh viễn — không có API nào lấy lại.
// Vì vậy client phải chuyển tiếp tên lên server ở lần đầu, và server lưu ngay.
func (s *Service) SignInWithOIDC(ctx context.Context, p Provider, idToken, nonce, firstAuthName string) (string, User, error) {
	v, ok := s.verifiers[p]
	if !ok {
		return "", User{}, fmt.Errorf("nhà cung cấp %q chưa được cấu hình", p)
	}

	id, err := v.Verify(ctx, idToken, nonce)
	if err != nil {
		return "", User{}, err
	}
	if firstAuthName != "" {
		id.Name = firstAuthName
	}

	user, err := s.resolveUser(ctx, id)
	if err != nil {
		return "", User{}, err
	}

	token, err := s.issueSession(ctx, user.ID)
	if err != nil {
		return "", User{}, err
	}
	return token, user, nil
}

// resolveUser tìm hoặc tạo người dùng cho một danh tính đã xác minh.
//
// Quy tắc ghép tài khoản ở đây là quyết định bảo mật quan trọng nhất của package,
// nên nó được viết tường minh thay vì "tìm theo email rồi gắn vào".
//
// Kịch bản tấn công mà quy tắc này chặn: kẻ tấn công đăng ký tài khoản mật khẩu
// bằng email của nạn nhân (chưa xác minh). Sau đó nạn nhân đăng nhập bằng Google
// với chính email đó. Nếu tự động ghép, kẻ tấn công đang giữ mật khẩu của một tài
// khoản mà nạn nhân vừa mang dữ liệu thật vào.
//
// Vì vậy chỉ tự động ghép khi CẢ HAI phía đều có email đã xác minh. Trường hợp
// còn lại trả về ErrLinkRequiresSignIn để client yêu cầu người dùng đăng nhập
// bằng phương thức cũ rồi liên kết một cách tường minh.
func (s *Service) resolveUser(ctx context.Context, id Identity) (User, error) {
	// Khoá tra cứu là (provider, sub) — KHÔNG phải email. Email đổi được; sub thì không.
	user, err := s.repo.UserByIdentity(ctx, id.Provider, id.Subject)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, ErrUserNotFound) {
		return User{}, err
	}

	if id.Email != "" {
		existing, err := s.repo.UserByEmail(ctx, id.Email)
		switch {
		case err == nil:
			if !id.EmailVerified || !existing.EmailVerified {
				return User{}, ErrLinkRequiresSignIn
			}
			if err := s.repo.LinkIdentity(ctx, IdentityRecord{
				UserID: existing.ID, Provider: id.Provider, Subject: id.Subject, Email: id.Email,
			}); err != nil {
				return User{}, err
			}
			return existing, nil
		case !errors.Is(err, ErrUserNotFound):
			return User{}, err
		}
	}

	created, err := s.repo.CreateUser(ctx, id.Email, id.Name, id.EmailVerified)
	if err != nil {
		return User{}, err
	}
	if err := s.repo.LinkIdentity(ctx, IdentityRecord{
		UserID: created.ID, Provider: id.Provider, Subject: id.Subject, Email: id.Email,
	}); err != nil {
		return User{}, err
	}
	return created, nil
}

// minPasswordLength 12 ký tự.
//
// Cố ý không áp luật "phải có hoa, số, ký tự đặc biệt": các luật đó đẩy người dùng
// tới những mật khẩu ngắn và dễ đoán kiểu "Password1!", trong khi độ dài mới là
// yếu tố quyết định thực sự. Đây cũng là khuyến nghị hiện hành của NIST.
const minPasswordLength = 12

func (s *Service) SignUpWithPassword(ctx context.Context, email, password, name string) (string, User, error) {
	email = normalizeEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return "", User{}, fmt.Errorf("email không hợp lệ")
	}
	if len([]rune(password)) < minPasswordLength {
		return "", User{}, fmt.Errorf("%w: cần ít nhất %d ký tự", ErrWeakPassword, minPasswordLength)
	}

	if _, err := s.repo.UserByEmail(ctx, email); err == nil {
		return "", User{}, ErrEmailTaken
	} else if !errors.Is(err, ErrUserNotFound) {
		return "", User{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", User{}, err
	}

	// emailVerified = false: chưa gửi email xác minh. Điều này CÓ HỆ QUẢ — tài
	// khoản mật khẩu chưa xác minh sẽ không được tự động ghép với Apple/Google.
	// Đó là chủ ý, xem resolveUser.
	user, err := s.repo.CreateUser(ctx, email, name, false)
	if err != nil {
		return "", User{}, err
	}
	if err := s.repo.SetPasswordHash(ctx, user.ID, hash); err != nil {
		return "", User{}, err
	}
	if err := s.repo.LinkIdentity(ctx, IdentityRecord{
		UserID: user.ID, Provider: ProviderPassword, Subject: email, Email: email,
	}); err != nil {
		return "", User{}, err
	}

	token, err := s.issueSession(ctx, user.ID)
	if err != nil {
		return "", User{}, err
	}
	return token, user, nil
}

func (s *Service) SignInWithPassword(ctx context.Context, email, password string) (string, User, error) {
	email = normalizeEmail(email)

	user, err := s.repo.UserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// So sánh với một hash giả để thời gian phản hồi của "email không tồn
			// tại" và "sai mật khẩu" gần bằng nhau. Không làm việc này thì kẻ tấn
			// công dò được email nào đã đăng ký chỉ bằng cách đo thời gian —
			// bcrypt cost 12 mất ~250ms, còn tra cứu hụt thì gần như tức thì.
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
			return "", User{}, ErrWrongCredentials
		}
		return "", User{}, err
	}

	hash, err := s.repo.PasswordHash(ctx, user.ID)
	if err != nil || len(hash) == 0 {
		// Tài khoản tồn tại nhưng chỉ đăng nhập bằng Apple/Google. Vẫn trả về lỗi
		// chung chung: nói "tài khoản này dùng Google" là tiết lộ thông tin.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", User{}, ErrWrongCredentials
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil {
		return "", User{}, ErrWrongCredentials
	}

	token, err := s.issueSession(ctx, user.ID)
	if err != nil {
		return "", User{}, err
	}
	return token, user, nil
}

// issueSession sinh token phiên dạng mờ (opaque).
//
// Chọn token mờ thay vì JWT tự ký: JWT không thu hồi được trước khi hết hạn, nên
// người dùng bấm "đăng xuất khỏi mọi thiết bị" sau khi mất máy sẽ không có tác
// dụng thật. Đánh đổi là mỗi request phải tra cơ sở dữ liệu — chấp nhận được ở
// quy mô này, và thêm cache sau vẫn dễ hơn là gỡ JWT ra.
//
// Chỉ HASH của token được lưu. Nếu cơ sở dữ liệu bị lộ, kẻ tấn công không mạo
// danh được phiên nào — đúng lý do người ta không lưu mật khẩu dạng thô.
func (s *Service) issueSession(ctx context.Context, userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("sinh token phiên: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	h := hashToken(token)

	if err := s.repo.CreateSession(ctx, userID, h, s.now().Add(s.sessionTTL)); err != nil {
		return "", err
	}
	return token, nil
}

// Authenticate đổi token phiên lấy người dùng.
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrSessionInvalid
	}
	rec, err := s.repo.SessionByTokenHash(ctx, hashToken(token))
	if err != nil {
		return User{}, ErrSessionInvalid
	}
	if s.now().After(rec.ExpiresAt) {
		// Dọn luôn thay vì để rác tích lại.
		_ = s.repo.DeleteSession(ctx, hashToken(token))
		return User{}, ErrSessionInvalid
	}
	return s.repo.UserByID(ctx, rec.UserID)
}

func (s *Service) SignOut(ctx context.Context, token string) error {
	return s.repo.DeleteSession(ctx, hashToken(token))
}

// SignOutEverywhere phục vụ tình huống mất thiết bị. Đây chính là khả năng mà
// JWT tự ký không cho được.
func (s *Service) SignOutEverywhere(ctx context.Context, userID string) error {
	return s.repo.DeleteSessionsOfUser(ctx, userID)
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// EqualTokenHash so sánh trong thời gian hằng định. Tầng lưu trữ dùng khi phải
// duyệt tuyến tính; bản Postgres tra bằng chỉ mục nên không cần.
func EqualTokenHash(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

func normalizeEmail(e string) string {
	return strings.ToLower(strings.TrimSpace(e))
}
