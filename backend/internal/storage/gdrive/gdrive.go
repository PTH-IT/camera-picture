// Package gdrive là provider lưu ảnh vào Google Drive CỦA CHÍNH NGƯỜI DÙNG.
//
// Vì sao provider này quan trọng về mặt kinh doanh chứ không chỉ kỹ thuật: khi
// người dùng lưu vào Drive của họ, ta không bán dung lượng nào cả — họ mua từ
// Google, ta chỉ bán chức năng. Không có giao dịch số nào để Apple thu 15-30%
// hoa hồng. Xem docs/adr/0002-auth-and-storage.md.
//
// RÀNG BUỘC CỨNG: chỉ dùng scope drive.file. Scope rộng hơn (drive.readonly hay
// drive đầy đủ) là restricted scope, kéo theo kiểm định bảo mật CASA tốn tiền và
// phải làm lại mỗi 12 tháng. drive.file chỉ cho phép app thấy file DO CHÍNH NÓ
// tạo — đủ cho mọi thứ ở đây, và không đủ để đọc thư viện ảnh sẵn có của người
// dùng. Nếu có ai đề xuất "cho người dùng duyệt thư mục Drive có sẵn", đó không
// phải tính năng nhỏ.
package gdrive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/hauph/camera/backend/internal/storage"
)

// ScopeDriveFile là scope DUY NHẤT được phép dùng. Xem chú thích của package.
const ScopeDriveFile = "https://www.googleapis.com/auth/drive.file"

// Endpoint OAuth của Google.
//
// Khai hằng ở đây thay vì import golang.org/x/oauth2/google: gói đó kéo theo
// cloud.google.com/go/compute/metadata (để dò credential trên hạ tầng GCP) —
// cả một cây phụ thuộc chỉ để lấy hai chuỗi cố định.
const (
	googleTokenURL = "https://oauth2.googleapis.com/token"
	googleAuthURL  = "https://accounts.google.com/o/oauth2/auth"
)

var (
	ErrNoRefreshToken = errors.New("chưa có refresh token — người dùng cần liên kết lại Drive")
	ErrDriveAPI       = errors.New("lỗi từ Google Drive API")
)

// TokenStore lưu refresh token đã mã hoá và thư mục gốc của từng người dùng.
//
// Bản triển khai thật phải mã hoá refresh token trước khi ghi — xem
// internal/secrets. Refresh token cho quyền đọc ghi Drive của người dùng gần như
// vô thời hạn; lộ cơ sở dữ liệu mà token ở dạng thô là lộ Drive của tất cả người
// dùng đã liên kết, và không thu hồi được bằng cách đổi mật khẩu.
type TokenStore interface {
	RefreshToken(ctx context.Context, userID string) (string, error)
	SaveRefreshToken(ctx context.Context, userID, token string) error
	RootFolderID(ctx context.Context, userID string) (string, error)
	SaveRootFolderID(ctx context.Context, userID, folderID string) error
}

type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string

	// Các endpoint tách ra để test được bằng httptest thay vì gọi Google thật.
	// Rỗng thì dùng endpoint thật.
	TokenURL  string
	APIBase   string
	UploadURL string

	// FolderName là thư mục app tạo trong Drive của người dùng.
	FolderName string
}

func (c Config) tokenURL() string {
	if c.TokenURL != "" {
		return c.TokenURL
	}
	return googleTokenURL
}

func (c Config) apiBase() string {
	if c.APIBase != "" {
		return c.APIBase
	}
	return "https://www.googleapis.com/drive/v3"
}

func (c Config) uploadURL() string {
	if c.UploadURL != "" {
		return c.UploadURL
	}
	return "https://www.googleapis.com/upload/drive/v3/files"
}

type Store struct {
	cfg    Config
	tokens TokenStore
	http   *http.Client

	// folders nhớ id thư mục đã tạo, khoá theo (thư mục cha, tên).
	//
	// Một buổi chụp đẩy lên hàng trăm file vào CÙNG hai thư mục. Không nhớ lại
	// thì mỗi file tốn thêm bốn lượt gọi Drive chỉ để hỏi đi hỏi lại hai cái tên
	// giống hệt nhau — chậm, và ăn vào hạn mức API.
	folderMu sync.Mutex
	folders  map[string]string
}

func New(cfg Config, tokens TokenStore, httpClient *http.Client) *Store {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.FolderName == "" {
		cfg.FolderName = "Camera Picture"
	}
	return &Store{cfg: cfg, tokens: tokens, http: httpClient, folders: map[string]string{}}
}

func (s *Store) ID() storage.ProviderID { return storage.ProviderGoogleDrive }

// Capabilities: có serverSideRender vì ta giữ refresh token nên đọc được file để
// kết xuất RAW. KHÔNG có enforcedQuota (hạn mức là chuyện giữa người dùng và
// Google) và KHÔNG có durable (người dùng thu hồi quyền hoặc xoá file là mất).
func (s *Store) Capabilities() []storage.Capability {
	return []storage.Capability{storage.CapServerSideRender}
}

func (s *Store) oauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.cfg.ClientID,
		ClientSecret: s.cfg.ClientSecret,
		RedirectURL:  s.cfg.RedirectURI,
		Scopes:       []string{ScopeDriveFile},
		Endpoint: oauth2.Endpoint{
			AuthURL:   googleAuthURL,
			TokenURL:  s.cfg.tokenURL(),
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
}

// AuthURL dựng URL để người dùng cấp quyền.
//
// access_type=offline và prompt=consent là BẮT BUỘC để nhận refresh token.
// Không có chúng, Google chỉ trả access token sống một giờ, và sau đó server mất
// khả năng đọc file — nghĩa là mất luôn kết xuất RAW phía máy chủ. Đây là lỗi rất
// hay gặp và chỉ lộ ra sau một giờ, khi mọi thứ đã trông như chạy được.
func (s *Store) AuthURL(state string) string {
	return s.oauthConfig().AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
}

// Link đổi mã uỷ quyền lấy refresh token và lưu lại.
func (s *Store) Link(ctx context.Context, userID, code string) error {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, s.http)

	tok, err := s.oauthConfig().Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("đổi mã uỷ quyền: %w", err)
	}
	if tok.RefreshToken == "" {
		// Google chỉ trả refresh token khi có access_type=offline và người dùng
		// thực sự thấy màn hình đồng ý. Nếu họ đã cấp quyền trước đó và ta không
		// gửi prompt=consent, trường này rỗng — và mọi thứ sẽ chạy đúng đúng một
		// giờ rồi hỏng.
		return fmt.Errorf("%w: Google không trả refresh token, kiểm tra access_type=offline và prompt=consent",
			ErrNoRefreshToken)
	}
	if err := s.tokens.SaveRefreshToken(ctx, userID, tok.RefreshToken); err != nil {
		return err
	}

	folderID, err := s.ensureFolder(ctx, userID, tok.AccessToken)
	if err != nil {
		return err
	}
	return s.tokens.SaveRootFolderID(ctx, userID, folderID)
}

// accessToken lấy access token còn hạn, tự làm mới từ refresh token.
func (s *Store) accessToken(ctx context.Context, userID string) (string, error) {
	rt, err := s.tokens.RefreshToken(ctx, userID)
	if err != nil {
		return "", err
	}
	if rt == "" {
		return "", ErrNoRefreshToken
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, s.http)
	ts := s.oauthConfig().TokenSource(ctx, &oauth2.Token{RefreshToken: rt})
	tok, err := ts.Token()
	if err != nil {
		// Refresh thất bại thường nghĩa là người dùng đã thu hồi quyền ở phía
		// Google. Không có cách nào tự khắc phục — phải báo để giao diện yêu cầu
		// liên kết lại, chứ không im lặng trả lỗi kỹ thuật khó hiểu.
		return "", fmt.Errorf("%w: %v", ErrNoRefreshToken, err)
	}
	return tok.AccessToken, nil
}

// ensureFolder tạo thư mục của app nếu chưa có.
//
// Với scope drive.file, app chỉ thấy được thứ do chính nó tạo — kể cả thư mục.
// Nên không thể "tìm thư mục có sẵn tên X"; phải tạo và nhớ id.
func (s *Store) ensureFolder(ctx context.Context, userID, accessToken string) (string, error) {
	if id, err := s.tokens.RootFolderID(ctx, userID); err == nil && id != "" {
		return id, nil
	}

	body, _ := json.Marshal(map[string]any{
		"name":     s.cfg.FolderName,
		"mimeType": "application/vnd.google-apps.folder",
	})
	var out struct {
		ID string `json:"id"`
	}
	if err := s.call(ctx, accessToken, http.MethodPost, s.cfg.apiBase()+"/files", body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// ensurePath tạo (hoặc tìm lại) chuỗi thư mục của một khoá, trả về id thư mục
// chứa file và tên file.
//
// Idempotent theo tên: gọi lại với cùng đường dẫn phải trả về đúng thư mục cũ,
// không tạo bản trùng. Drive CHO PHÉP hai thư mục trùng tên trong cùng thư mục
// cha — đó là lý do phải hỏi trước khi tạo, và là lỗi rất dễ mắc.
func (s *Store) ensurePath(ctx context.Context, userID, token, rootID, key string) (string, string, error) {
	parts := strings.Split(key, "/")
	name := parts[len(parts)-1]
	if name == "" {
		return "", "", fmt.Errorf("%w: khoá không có tên file", storage.ErrUnsupported)
	}

	parentID := rootID
	for _, dir := range parts[:len(parts)-1] {
		dir = safeName(dir)
		if dir == "" {
			continue
		}
		id, err := s.ensureChildFolder(ctx, userID, token, parentID, dir)
		if err != nil {
			return "", "", err
		}
		parentID = id
	}
	return parentID, safeName(name), nil
}

func (s *Store) ensureChildFolder(ctx context.Context, userID, token, parentID, name string) (string, error) {
	cacheKey := userID + "\x00" + parentID + "\x00" + name

	s.folderMu.Lock()
	if id, ok := s.folders[cacheKey]; ok {
		s.folderMu.Unlock()
		return id, nil
	}
	s.folderMu.Unlock()

	// Tìm trước. Dấu nháy đơn trong tên phải escape, nếu không một buổi chụp tên
	// "Mai's wedding" sẽ làm hỏng cú pháp truy vấn của Drive.
	q := fmt.Sprintf(
		"name = '%s' and '%s' in parents and mimeType = 'application/vnd.google-apps.folder' and trashed = false",
		strings.ReplaceAll(name, "'", `\'`), parentID)
	u := s.cfg.apiBase() + "/files?fields=files(id)&pageSize=1&q=" + url.QueryEscape(q)

	var found struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	if err := s.call(ctx, token, http.MethodGet, u, nil, &found); err != nil {
		return "", err
	}

	id := ""
	if len(found.Files) > 0 {
		id = found.Files[0].ID
	} else {
		body, _ := json.Marshal(map[string]any{
			"name":     name,
			"mimeType": "application/vnd.google-apps.folder",
			"parents":  []string{parentID},
		})
		var out struct {
			ID string `json:"id"`
		}
		if err := s.call(ctx, token, http.MethodPost, s.cfg.apiBase()+"/files", body, &out); err != nil {
			return "", err
		}
		id = out.ID
	}

	s.folderMu.Lock()
	s.folders[cacheKey] = id
	s.folderMu.Unlock()
	return id, nil
}

// Upload tạo phiên tải lên có thể tiếp tục (resumable) và trả URI cho client.
//
// Client PUT bytes THẲNG lên Google. Server không bao giờ chạm vào dữ liệu ảnh —
// cho một NEF 60MB chảy qua handler sẽ giữ goroutine và băng thông suốt thời gian
// tải, và với hàng chục client đang tether cùng lúc thì đó là cách làm sập service.
//
// Chọn resumable thay vì tải một lần: buổi chụp thật hay mất mạng giữa chừng, và
// bắt tải lại từ đầu một file 60MB qua mạng di động là không chấp nhận được.
func (s *Store) Upload(ctx context.Context, userID, key string, size int64) (storage.Target, error) {
	token, err := s.accessToken(ctx, userID)
	if err != nil {
		return storage.Target{}, err
	}
	folderID, err := s.tokens.RootFolderID(ctx, userID)
	if err != nil || folderID == "" {
		return storage.Target{}, fmt.Errorf("%w: chưa có thư mục gốc", storage.ErrNotLinked)
	}

	// Khoá là một ĐƯỜNG DẪN: "2026-08-30 Minh & Lan/goc/DSC_4001.NEF". Với S3
	// dấu gạch chéo chỉ là quy ước hiển thị, còn Drive thì không có khái niệm
	// đó — phải tạo thư mục thật cho từng cấp, nếu không toàn bộ buổi chụp đổ
	// dồn vào một thư mục phẳng và người dùng không tìm nổi ảnh của mình.
	parentID, name, err := s.ensurePath(ctx, userID, token, folderID, key)
	if err != nil {
		return storage.Target{}, err
	}

	meta, _ := json.Marshal(map[string]any{
		"name":    name,
		"parents": []string{parentID},
	})

	u := s.cfg.uploadURL() + "?uploadType=resumable"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(meta))
	if err != nil {
		return storage.Target{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	if size > 0 {
		req.Header.Set("X-Upload-Content-Length", fmt.Sprint(size))
	}

	resp, err := s.http.Do(req)
	if err != nil {
		return storage.Target{}, fmt.Errorf("%w: %v", ErrDriveAPI, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return storage.Target{}, apiError(resp)
	}
	sessionURI := resp.Header.Get("Location")
	if sessionURI == "" {
		return storage.Target{}, fmt.Errorf("%w: thiếu header Location cho phiên resumable", ErrDriveAPI)
	}

	return storage.Target{
		Provider: storage.ProviderGoogleDrive,
		URL:      sessionURI,
		Method:   "PUT",
		// Phiên resumable đã mang sẵn quyền trong URI, nên client KHÔNG cần
		// Authorization — và không nên nhận access token nếu không bắt buộc.
		Key:       key,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour), // Google giữ phiên một tuần
	}, nil
}

// Download trả URL tải về kèm header Authorization.
//
// Drive KHÔNG có URL ký sẵn như S3, nên client buộc phải gửi access token. Đánh
// đổi được chấp nhận: token sống một giờ, và scope drive.file chỉ mở được đúng
// những file do app này tạo — chính app mobile cũng có thể tự xin quyền tương
// đương qua Google Sign-In. Nhưng đừng ghi Target này ra log.
func (s *Store) Download(ctx context.Context, userID, key string) (storage.Target, error) {
	token, err := s.accessToken(ctx, userID)
	if err != nil {
		return storage.Target{}, err
	}
	// key với Drive là fileId do Google cấp, ghi lại lúc Confirm.
	return storage.Target{
		Provider: storage.ProviderGoogleDrive,
		URL:      s.cfg.apiBase() + "/files/" + url.PathEscape(key) + "?alt=media",
		Method:   "GET",
		Headers:  map[string]string{"Authorization": "Bearer " + token},
		Key:      key,
		// Access token của Google sống một giờ. Trừ hao để client không bắt đầu
		// một lượt tải bằng token sắp hết hạn giữa chừng.
		ExpiresAt: time.Now().Add(50 * time.Minute),
	}, nil
}

func (s *Store) Delete(ctx context.Context, userID, key string) error {
	token, err := s.accessToken(ctx, userID)
	if err != nil {
		return err
	}
	return s.call(ctx, token, http.MethodDelete,
		s.cfg.apiBase()+"/files/"+url.PathEscape(key), nil, nil)
}

// Usage đọc hạn mức Drive của người dùng.
//
// Enforced = false: đây là hạn mức của GOOGLE, không phải của ta. Ta chỉ đọc để
// hiển thị. Người dùng hết dung lượng Drive thì upload sẽ hỏng ở phía Google, và
// giao diện cần nói rõ điều đó thay vì báo lỗi chung.
func (s *Store) Usage(ctx context.Context, userID string) (storage.Usage, error) {
	token, err := s.accessToken(ctx, userID)
	if err != nil {
		return storage.Usage{}, err
	}

	var out struct {
		StorageQuota struct {
			Limit string `json:"limit"`
			Usage string `json:"usage"`
		} `json:"storageQuota"`
	}
	u := s.cfg.apiBase() + "/about?fields=storageQuota"
	if err := s.call(ctx, token, http.MethodGet, u, nil, &out); err != nil {
		return storage.Usage{}, err
	}

	return storage.Usage{
		Provider: storage.ProviderGoogleDrive,
		// Google trả số dưới dạng CHUỖI vì chúng vượt quá số nguyên an toàn của
		// JavaScript. Giải mã thẳng vào int64 sẽ lỗi.
		UsedBytes:  parseInt64(out.StorageQuota.Usage),
		LimitBytes: parseInt64(out.StorageQuota.Limit),
		Enforced:   false,
	}, nil
}

func (s *Store) call(ctx context.Context, token, method, u string, body []byte, out any) error {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDriveAPI, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return apiError(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func apiError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	return fmt.Errorf("%w: %d %s", ErrDriveAPI, resp.StatusCode, strings.TrimSpace(string(b)))
}

// safeName lấy phần tên file từ khoá.
//
// Drive dùng id chứ không dùng đường dẫn, nên "thư mục" trong khoá không có ý
// nghĩa gì ở đây — chỉ giữ tên để người dùng nhận ra file khi mở Drive.
func safeName(key string) string {
	if i := strings.LastIndex(key, "/"); i >= 0 && i < len(key)-1 {
		return key[i+1:]
	}
	if key == "" {
		return "untitled"
	}
	return key
}

func parseInt64(s string) int64 {
	var v int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		v = v*10 + int64(c-'0')
	}
	return v
}

var _ storage.Provider = (*Store)(nil)
