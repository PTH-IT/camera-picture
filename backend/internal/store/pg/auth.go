package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hauph/camera/backend/internal/auth"
	"github.com/hauph/camera/backend/internal/ids"
)

// AuthRepo là bản Postgres của auth.Repo.
type AuthRepo struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewAuthRepo(pool *pgxpool.Pool, now func() time.Time) *AuthRepo {
	if now == nil {
		now = time.Now
	}
	return &AuthRepo{pool: pool, now: now}
}

func (r *AuthRepo) UserByIdentity(ctx context.Context, p auth.Provider, subject string) (auth.User, error) {
	return r.scanUser(ctx, `
		SELECT u.id, u.email, u.email_verified, u.name, u.created_at
		FROM users u
		JOIN identities i ON i.user_id = u.id
		WHERE i.provider = $1 AND i.subject = $2`, string(p), subject)
}

func (r *AuthRepo) UserByEmail(ctx context.Context, email string) (auth.User, error) {
	// email rỗng KHÔNG được coi là truy vấn hợp lệ: người dùng Apple ẩn email có
	// email NULL, và "tìm user có email rỗng" sẽ ghép nhầm tất cả họ vào một
	// người. Trả về không tìm thấy ngay.
	if email == "" {
		return auth.User{}, auth.ErrUserNotFound
	}
	return r.scanUser(ctx, `
		SELECT id, email, email_verified, name, created_at
		FROM users WHERE email = $1`, email)
}

func (r *AuthRepo) UserByID(ctx context.Context, id string) (auth.User, error) {
	return r.scanUser(ctx, `
		SELECT id, email, email_verified, name, created_at
		FROM users WHERE id = $1`, id)
}

func (r *AuthRepo) scanUser(ctx context.Context, q string, args ...any) (auth.User, error) {
	var u auth.User
	var email, name *string
	err := r.pool.QueryRow(ctx, q, args...).
		Scan(&u.ID, &email, &u.EmailVerified, &name, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrUserNotFound
	}
	if err != nil {
		if isInvalidUUID(err) {
			return auth.User{}, auth.ErrUserNotFound
		}
		return auth.User{}, fmt.Errorf("đọc người dùng: %w", err)
	}
	if email != nil {
		u.Email = *email
	}
	if name != nil {
		u.Name = *name
	}
	return u, nil
}

func (r *AuthRepo) CreateUser(ctx context.Context, email, name string, emailVerified bool) (auth.User, error) {
	u := auth.User{
		ID: ids.New(), Email: email, EmailVerified: emailVerified,
		Name: name, CreatedAt: r.now(),
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO users (id, email, email_verified, name, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		u.ID, nullIfEmpty(email), emailVerified, nullIfEmpty(name), u.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return auth.User{}, auth.ErrEmailTaken
		}
		return auth.User{}, fmt.Errorf("tạo người dùng: %w", err)
	}
	return u, nil
}

func (r *AuthRepo) LinkIdentity(ctx context.Context, rec auth.IdentityRecord) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO identities (provider, subject, user_id, email)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (provider, subject) DO UPDATE SET email = EXCLUDED.email`,
		string(rec.Provider), rec.Subject, rec.UserID, nullIfEmpty(rec.Email))
	if err != nil {
		return fmt.Errorf("liên kết danh tính: %w", err)
	}
	return nil
}

func (r *AuthRepo) IdentitiesOf(ctx context.Context, userID string) ([]auth.IdentityRecord, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT provider, subject, user_id, email FROM identities WHERE user_id = $1`, userID)
	if err != nil {
		if isInvalidUUID(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("đọc danh tính: %w", err)
	}
	defer rows.Close()

	var out []auth.IdentityRecord
	for rows.Next() {
		var rec auth.IdentityRecord
		var provider string
		var email *string
		if err := rows.Scan(&provider, &rec.Subject, &rec.UserID, &email); err != nil {
			return nil, err
		}
		rec.Provider = auth.Provider(provider)
		if email != nil {
			rec.Email = *email
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *AuthRepo) SetPasswordHash(ctx context.Context, userID string, hash []byte) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_passwords (user_id, password_hash, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET
			password_hash = EXCLUDED.password_hash,
			updated_at = EXCLUDED.updated_at`,
		userID, hash, r.now())
	if err != nil {
		return fmt.Errorf("ghi mật khẩu: %w", err)
	}
	return nil
}

func (r *AuthRepo) PasswordHash(ctx context.Context, userID string) ([]byte, error) {
	var hash []byte
	err := r.pool.QueryRow(ctx, `SELECT password_hash FROM user_passwords WHERE user_id = $1`, userID).
		Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrUserNotFound
	}
	if err != nil {
		if isInvalidUUID(err) {
			return nil, auth.ErrUserNotFound
		}
		return nil, fmt.Errorf("đọc mật khẩu: %w", err)
	}
	return hash, nil
}

func (r *AuthRepo) CreateSession(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO sessions_auth (token_hash, user_id, expires_at, created_at)
		VALUES ($1, $2, $3, $4)`,
		tokenHash, userID, expiresAt, r.now())
	if err != nil {
		return fmt.Errorf("tạo phiên: %w", err)
	}
	return nil
}

func (r *AuthRepo) SessionByTokenHash(ctx context.Context, tokenHash []byte) (auth.SessionRecord, error) {
	var rec auth.SessionRecord
	err := r.pool.QueryRow(ctx, `
		SELECT user_id, expires_at FROM sessions_auth WHERE token_hash = $1`, tokenHash).
		Scan(&rec.UserID, &rec.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.SessionRecord{}, auth.ErrSessionInvalid
	}
	if err != nil {
		return auth.SessionRecord{}, fmt.Errorf("đọc phiên: %w", err)
	}
	return rec, nil
}

func (r *AuthRepo) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions_auth WHERE token_hash = $1`, tokenHash)
	return err
}

// DeleteSessionsOfUser là thứ khiến "đăng xuất khỏi mọi thiết bị" có tác dụng
// thật — khả năng mà JWT tự ký không cho được.
func (r *AuthRepo) DeleteSessionsOfUser(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions_auth WHERE user_id = $1`, userID)
	if err != nil && isInvalidUUID(err) {
		return nil
	}
	return err
}

// PurgeExpiredSessions dọn phiên hết hạn.
//
// Cần chạy định kỳ: Authenticate có xoá phiên hết hạn khi gặp, nhưng phiên của
// người dùng không bao giờ quay lại thì không ai gặp, và bảng sẽ phình mãi.
func (r *AuthRepo) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM sessions_auth WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

var _ auth.Repo = (*AuthRepo)(nil)
