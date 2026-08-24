package gdrive

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/hauph/camera/backend/internal/storage"
)

// Test chạy với một Google giả dựng bằng httptest. Mục tiêu không phải là kiểm
// tra Google hoạt động đúng, mà là kiểm tra CÁCH TA GỌI nó: đúng scope, đúng
// tham số để nhận refresh token, và xử lý đúng các dạng dữ liệu lạ mà Google trả
// về (số dưới dạng chuỗi, thiếu refresh token).

type memTokens struct {
	mu      sync.Mutex
	refresh map[string]string
	folders map[string]string
}

func newMemTokens() *memTokens {
	return &memTokens{refresh: map[string]string{}, folders: map[string]string{}}
}

func (m *memTokens) RefreshToken(_ context.Context, userID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.refresh[userID]
	if !ok {
		return "", ErrNoRefreshToken
	}
	return t, nil
}

func (m *memTokens) SaveRefreshToken(_ context.Context, userID, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refresh[userID] = token
	return nil
}

func (m *memTokens) RootFolderID(_ context.Context, userID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.folders[userID], nil
}

func (m *memTokens) SaveRootFolderID(_ context.Context, userID, folderID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.folders[userID] = folderID
	return nil
}

// fakeGoogle ghi lại request để test khẳng định được ta gọi ĐÚNG cách.
type fakeGoogle struct {
	srv *httptest.Server

	mu             sync.Mutex
	tokenParams    url.Values
	withRefresh    bool
	lastUploadMeta map[string]any
	uploadAuth     string
}

func newFakeGoogle(t *testing.T, withRefresh bool) *fakeGoogle {
	t.Helper()
	g := &fakeGoogle{withRefresh: withRefresh}

	mux := http.NewServeMux()

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		g.mu.Lock()
		g.tokenParams = r.Form
		g.mu.Unlock()

		body := map[string]any{
			"access_token": "access-token-gia",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}
		if g.withRefresh {
			body["refresh_token"] = "refresh-token-gia"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})

	mux.HandleFunc("POST /drive/v3/files", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "folder-123"})
	})

	mux.HandleFunc("DELETE /drive/v3/files/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /drive/v3/about", func(w http.ResponseWriter, _ *http.Request) {
		// Google trả số dưới dạng CHUỖI vì chúng vượt quá số nguyên an toàn của
		// JavaScript. Đây chính là cái bẫy mà parseInt64 tồn tại để xử lý.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"storageQuota": map[string]string{
				"limit": "16106127360",
				"usage": "1073741824",
			},
		})
	})

	mux.HandleFunc("POST /upload", func(w http.ResponseWriter, r *http.Request) {
		var meta map[string]any
		_ = json.NewDecoder(r.Body).Decode(&meta)
		g.mu.Lock()
		g.lastUploadMeta = meta
		g.uploadAuth = r.Header.Get("Authorization")
		g.mu.Unlock()

		w.Header().Set("Location", "https://upload.example/session/abc123")
		w.WriteHeader(http.StatusOK)
	})

	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

func newTestStore(t *testing.T, withRefresh bool) (*Store, *memTokens, *fakeGoogle) {
	t.Helper()
	g := newFakeGoogle(t, withRefresh)
	tokens := newMemTokens()
	s := New(Config{
		ClientID: "client-id", ClientSecret: "secret", RedirectURI: "app://cb",
		TokenURL:  g.srv.URL + "/token",
		APIBase:   g.srv.URL + "/drive/v3",
		UploadURL: g.srv.URL + "/upload",
	}, tokens, g.srv.Client())
	return s, tokens, g
}

// TestOnlyDriveFileScope: scope là ràng buộc kiến trúc cứng. drive.readonly hay
// drive đầy đủ là restricted scope, kéo theo kiểm định CASA tốn tiền và phải làm
// lại mỗi 12 tháng. Test này canh chừng việc ai đó "mở rộng một chút cho tiện".
func TestOnlyDriveFileScope(t *testing.T) {
	s, _, _ := newTestStore(t, true)

	authURL := s.AuthURL("state-123")
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	scopes := strings.Fields(u.Query().Get("scope"))

	if len(scopes) != 1 || scopes[0] != ScopeDriveFile {
		t.Fatalf("scope = %v, chỉ được phép [%s] — scope rộng hơn kéo theo kiểm định CASA",
			scopes, ScopeDriveFile)
	}
}

// TestAuthURLRequestsOfflineAccess: thiếu access_type=offline hoặc prompt=consent
// thì Google chỉ trả access token sống một giờ. Mọi thứ trông như chạy được, rồi
// một giờ sau server mất khả năng đọc file và kết xuất RAW phía máy chủ chết.
func TestAuthURLRequestsOfflineAccess(t *testing.T) {
	s, _, _ := newTestStore(t, true)

	u, err := url.Parse(s.AuthURL("state-123"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if q.Get("access_type") != "offline" {
		t.Errorf("access_type = %q, muốn offline", q.Get("access_type"))
	}
	if q.Get("prompt") != "consent" {
		t.Errorf("prompt = %q, muốn consent", q.Get("prompt"))
	}
	if q.Get("state") != "state-123" {
		t.Errorf("state = %q — thiếu state là mở đường cho CSRF ở luồng OAuth", q.Get("state"))
	}
}

func TestLinkStoresRefreshTokenAndFolder(t *testing.T) {
	ctx := context.Background()
	s, tokens, _ := newTestStore(t, true)

	if err := s.Link(ctx, "u1", "ma-uy-quyen"); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if got, _ := tokens.RefreshToken(ctx, "u1"); got != "refresh-token-gia" {
		t.Errorf("refresh token = %q", got)
	}
	if got, _ := tokens.RootFolderID(ctx, "u1"); got != "folder-123" {
		t.Errorf("thư mục gốc = %q", got)
	}
}

// TestLinkFailsLoudlyWithoutRefreshToken: Google im lặng bỏ qua refresh_token khi
// người dùng đã cấp quyền trước đó và ta không gửi prompt=consent. Nếu ta cũng im
// lặng chấp nhận, liên kết trông như thành công và hỏng sau đúng một giờ — kiểu
// lỗi tốn nhiều giờ để truy vết vì triệu chứng cách xa nguyên nhân.
func TestLinkFailsLoudlyWithoutRefreshToken(t *testing.T) {
	ctx := context.Background()
	s, tokens, _ := newTestStore(t, false)

	err := s.Link(ctx, "u1", "ma-uy-quyen")
	if !errors.Is(err, ErrNoRefreshToken) {
		t.Fatalf("lỗi = %v, muốn ErrNoRefreshToken", err)
	}
	if got, _ := tokens.RefreshToken(ctx, "u1"); got != "" {
		t.Error("đã lưu liên kết dù không có refresh token")
	}
}

func TestUploadReturnsResumableSession(t *testing.T) {
	ctx := context.Background()
	s, _, g := newTestStore(t, true)

	if err := s.Link(ctx, "u1", "ma"); err != nil {
		t.Fatalf("Link: %v", err)
	}

	target, err := s.Upload(ctx, "u1", "sessions/s1/DSC_0001.NEF", 60<<20)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if target.URL != "https://upload.example/session/abc123" {
		t.Errorf("URL = %q, muốn URI phiên resumable", target.URL)
	}
	if target.Provider != storage.ProviderGoogleDrive {
		t.Errorf("Provider = %q", target.Provider)
	}

	// URI phiên đã mang sẵn quyền, nên KHÔNG được kèm access token cho client.
	if _, ok := target.Headers["Authorization"]; ok {
		t.Error("Target upload kèm Authorization — thừa và làm lộ token không cần thiết")
	}

	g.mu.Lock()
	meta, auth := g.lastUploadMeta, g.uploadAuth
	g.mu.Unlock()

	if !strings.HasPrefix(auth, "Bearer ") {
		t.Errorf("request tạo phiên thiếu Authorization: %q", auth)
	}
	// Drive dùng id chứ không dùng đường dẫn, nên chỉ giữ tên file.
	if meta["name"] != "DSC_0001.NEF" {
		t.Errorf("tên file = %v, muốn DSC_0001.NEF", meta["name"])
	}
	parents, _ := meta["parents"].([]any)
	if len(parents) != 1 || parents[0] != "folder-123" {
		t.Errorf("parents = %v, muốn [folder-123]", parents)
	}
}

func TestUploadWithoutLinkFails(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newTestStore(t, true)

	if _, err := s.Upload(ctx, "chua-lien-ket", "a.NEF", 100); err == nil {
		t.Fatal("cho phép upload khi chưa liên kết Drive")
	}
}

func TestDownloadCarriesAuthorization(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newTestStore(t, true)
	if err := s.Link(ctx, "u1", "ma"); err != nil {
		t.Fatalf("Link: %v", err)
	}

	target, err := s.Download(ctx, "u1", "file-abc")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	// Drive không có URL ký sẵn như S3 — không có header thì client không tải
	// được gì. Đây là lý do Provider.Download trả Target chứ không trả string.
	if !strings.HasPrefix(target.Headers["Authorization"], "Bearer ") {
		t.Errorf("thiếu header Authorization: %v", target.Headers)
	}
	if !strings.Contains(target.URL, "alt=media") {
		t.Errorf("URL thiếu alt=media nên sẽ trả metadata thay vì nội dung: %q", target.URL)
	}
	// Token KHÔNG được nằm trong URL: URL bị ghi vào log truy cập, lịch sử trình
	// duyệt và header Referer.
	if strings.Contains(target.URL, "access-token-gia") {
		t.Error("access token nằm trong URL")
	}
}

// TestUsageParsesStringNumbers: Google trả hạn mức dưới dạng CHUỖI vì chúng vượt
// quá số nguyên an toàn của JavaScript. Giải mã thẳng vào int64 sẽ lỗi và toàn bộ
// màn hình dung lượng hỏng.
func TestUsageParsesStringNumbers(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newTestStore(t, true)
	if err := s.Link(ctx, "u1", "ma"); err != nil {
		t.Fatalf("Link: %v", err)
	}

	u, err := s.Usage(ctx, "u1")
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if u.LimitBytes != 16106127360 {
		t.Errorf("LimitBytes = %d, muốn 16106127360", u.LimitBytes)
	}
	if u.UsedBytes != 1073741824 {
		t.Errorf("UsedBytes = %d, muốn 1073741824", u.UsedBytes)
	}
	// Enforced = false vì đây là hạn mức của GOOGLE, ta chỉ đọc để hiển thị.
	if u.Enforced {
		t.Error("Enforced = true — ta không cưỡng chế được hạn mức Drive")
	}
}

// TestCapabilitiesReflectReality: Drive kết xuất phía máy chủ được (ta giữ refresh
// token), nhưng KHÔNG bền và KHÔNG cưỡng chế hạn mức được. Khai sai ở đây khiến
// giao diện hứa với người dùng những thứ không có.
func TestCapabilitiesReflectReality(t *testing.T) {
	s, _, _ := newTestStore(t, true)

	if !storage.Has(s, storage.CapServerSideRender) {
		t.Error("thiếu CapServerSideRender")
	}
	if storage.Has(s, storage.CapEnforcedQuota) {
		t.Error("khai CapEnforcedQuota — hạn mức Drive là của Google, không phải của ta")
	}
	if storage.Has(s, storage.CapDurable) {
		t.Error("khai CapDurable — người dùng thu hồi quyền hoặc xoá file là mất dữ liệu")
	}
}

func TestRevokedAccessSurfacesClearly(t *testing.T) {
	ctx := context.Background()
	s, tokens, _ := newTestStore(t, true)

	// Chưa từng liên kết.
	if _, err := s.Download(ctx, "u1", "file-abc"); !errors.Is(err, ErrNoRefreshToken) {
		t.Errorf("lỗi = %v, muốn ErrNoRefreshToken", err)
	}

	// Đã liên kết rồi bị thu hồi (token rỗng).
	_ = tokens.SaveRefreshToken(ctx, "u2", "")
	if _, err := s.Download(ctx, "u2", "file-abc"); !errors.Is(err, ErrNoRefreshToken) {
		t.Errorf("lỗi = %v, muốn ErrNoRefreshToken", err)
	}
}
