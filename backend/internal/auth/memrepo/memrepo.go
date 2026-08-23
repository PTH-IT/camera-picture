// Package memrepo là bản Repo lưu trong bộ nhớ cho tầng xác thực.
//
// Dùng cho test và chạy thử local. KHÔNG dùng cho production: không bền, không
// chịu được nhiều tiến trình. Nó tồn tại để các quyết định bảo mật trong
// auth.Service — quy tắc ghép tài khoản, vòng đời phiên — được test kỹ mà không
// cần dựng Postgres.
package memrepo

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hauph/camera/backend/internal/auth"
)

type Repo struct {
	mu sync.Mutex
	n  int

	users      map[string]auth.User
	byEmail    map[string]string
	identities map[string]auth.IdentityRecord // khoá: provider\x00subject
	passwords  map[string][]byte
	sessions   map[string]auth.SessionRecord // khoá: hex của token hash
	now        func() time.Time
}

func New(now func() time.Time) *Repo {
	if now == nil {
		now = time.Now
	}
	return &Repo{
		users:      map[string]auth.User{},
		byEmail:    map[string]string{},
		identities: map[string]auth.IdentityRecord{},
		passwords:  map[string][]byte{},
		sessions:   map[string]auth.SessionRecord{},
		now:        now,
	}
}

func identityKey(p auth.Provider, subject string) string {
	return string(p) + "\x00" + subject
}

func sessionKey(hash []byte) string {
	return fmt.Sprintf("%x", hash)
}

func (r *Repo) UserByIdentity(_ context.Context, p auth.Provider, subject string) (auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rec, ok := r.identities[identityKey(p, subject)]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	u, ok := r.users[rec.UserID]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return u, nil
}

func (r *Repo) UserByEmail(_ context.Context, email string) (auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id, ok := r.byEmail[email]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return r.users[id], nil
}

func (r *Repo) UserByID(_ context.Context, id string) (auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	u, ok := r.users[id]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return u, nil
}

func (r *Repo) CreateUser(_ context.Context, email, name string, emailVerified bool) (auth.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.n++
	u := auth.User{
		ID:            fmt.Sprintf("u-%03d", r.n),
		Email:         email,
		EmailVerified: emailVerified,
		Name:          name,
		CreatedAt:     r.now(),
	}
	r.users[u.ID] = u
	// Email rỗng là hợp lệ: người dùng Apple có thể ẩn email hoàn toàn.
	if email != "" {
		r.byEmail[email] = u.ID
	}
	return u, nil
}

func (r *Repo) LinkIdentity(_ context.Context, rec auth.IdentityRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.identities[identityKey(rec.Provider, rec.Subject)] = rec
	return nil
}

func (r *Repo) IdentitiesOf(_ context.Context, userID string) ([]auth.IdentityRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []auth.IdentityRecord
	for _, rec := range r.identities {
		if rec.UserID == userID {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (r *Repo) SetPasswordHash(_ context.Context, userID string, hash []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.passwords[userID] = hash
	return nil
}

func (r *Repo) PasswordHash(_ context.Context, userID string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	h, ok := r.passwords[userID]
	if !ok {
		return nil, auth.ErrUserNotFound
	}
	return h, nil
}

func (r *Repo) CreateSession(_ context.Context, userID string, tokenHash []byte, expiresAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sessions[sessionKey(tokenHash)] = auth.SessionRecord{UserID: userID, ExpiresAt: expiresAt}
	return nil
}

func (r *Repo) SessionByTokenHash(_ context.Context, tokenHash []byte) (auth.SessionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rec, ok := r.sessions[sessionKey(tokenHash)]
	if !ok {
		return auth.SessionRecord{}, auth.ErrSessionInvalid
	}
	return rec, nil
}

func (r *Repo) DeleteSession(_ context.Context, tokenHash []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.sessions, sessionKey(tokenHash))
	return nil
}

func (r *Repo) DeleteSessionsOfUser(_ context.Context, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for k, rec := range r.sessions {
		if rec.UserID == userID {
			delete(r.sessions, k)
		}
	}
	return nil
}

var _ auth.Repo = (*Repo)(nil)
