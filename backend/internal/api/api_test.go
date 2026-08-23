package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hauph/camera/backend/internal/auth"
	"github.com/hauph/camera/backend/internal/auth/memrepo"
	"github.com/hauph/camera/backend/internal/protocol"
	"github.com/hauph/camera/backend/internal/store"
	"github.com/hauph/camera/backend/internal/store/memory"
)

// testClient gói handler cùng token của một người dùng, để mọi request trong test
// đều đi qua đúng đường xác thực như production.
type testClient struct {
	h     http.Handler
	token string
}

func newTestServer() http.Handler {
	n := 0
	base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	tick := 0
	mem := memory.New(
		func() string { n++; return fmt.Sprintf("id-%03d", n) },
		func() time.Time { tick++; return base.Add(time.Duration(tick) * time.Second) },
	)
	authSvc := auth.NewService(memrepo.New(time.Now), map[auth.Provider]*auth.Verifier{}, time.Now)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(mem, authSvc, log).Routes()
}

// newAuthed dựng server kèm một người dùng đã đăng nhập.
func newAuthed(t *testing.T) *testClient {
	t.Helper()
	h := newTestServer()
	return signUpAs(t, h, "nguoi-dung@example.com")
}

func signUpAs(t *testing.T, h http.Handler, email string) *testClient {
	t.Helper()
	rec := do(t, h, "POST", "/v1/auth/signup", map[string]any{
		"email": email, "password": "mat-khau-du-dai-12", "name": "Người Dùng",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("đăng ký %s: %d %s", email, rec.Code, rec.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("giải mã token: %v", err)
	}
	return &testClient{h: h, token: out.Token}
}

func (c *testClient) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return doAuth(t, c.h, c.token, method, path, body)
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	return doAuth(t, h, "", method, path, body)
}

func doAuth(t *testing.T, h http.Handler, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeInto[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("giải mã %T: %v (body=%s)", out, err, rec.Body.String())
	}
	return out
}

func mkSession(t *testing.T, c *testClient) string {
	t.Helper()
	rec := c.do(t, "POST", "/v1/sessions", map[string]any{"name": "Đám cưới"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("tạo session: %d %s", rec.Code, rec.Body.String())
	}
	return decodeInto[store.Session](t, rec).ID
}

func TestHealthz(t *testing.T) {
	rec := do(t, newTestServer(), "GET", "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, muốn 200", rec.Code)
	}
}

// TestFullSyncFlow đi hết vòng đời một buổi chụp qua HTTP: tạo session, đẩy
// metadata, kéo delta, chỉnh sửa, xác nhận asset, đồng bộ lại.
func TestFullSyncFlow(t *testing.T) {
	c := newAuthed(t)
	sid := mkSession(t, c)

	images := []protocol.ImageInput{
		{ClientID: "c1", Filename: "DSC_0001.NEF", Format: protocol.FormatNEF,
			ByteSize: 55 << 20, CapturedAt: time.Now().UTC().Truncate(time.Second), IsRaw: true},
		{ClientID: "c2", Filename: "DSC_0002.NEF", Format: protocol.FormatNEF,
			ByteSize: 56 << 20, CapturedAt: time.Now().UTC().Truncate(time.Second), IsRaw: true},
	}

	rec := c.do(t, "POST", "/v1/sessions/"+sid+"/images/batch",
		protocol.BatchImagesRequest{Images: images})
	if rec.Code != http.StatusOK {
		t.Fatalf("batch: %d %s", rec.Code, rec.Body.String())
	}
	batch := decodeInto[protocol.BatchImagesResponse](t, rec)
	if batch.Created != 2 {
		t.Fatalf("created = %d, muốn 2", batch.Created)
	}

	// Kéo delta từ đầu.
	rec = c.do(t, "GET", "/v1/sessions/"+sid+"/changes?since=0", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("changes: %d %s", rec.Code, rec.Body.String())
	}
	ch := decodeInto[protocol.ChangesResponse](t, rec)
	if len(ch.Images) != 2 {
		t.Fatalf("lấy %d ảnh, muốn 2", len(ch.Images))
	}
	// Bất biến của kiến trúc: ảnh chưa lên server thì không có asset nào.
	if len(ch.Images[0].Assets) != 0 {
		t.Errorf("ảnh mới không được có asset, có %d", len(ch.Images[0].Assets))
	}

	imgID := batch.IDs["c1"]

	rec = c.do(t, "PUT", "/v1/images/"+imgID+"/edit",
		protocol.PutEditRequest{Rating: 5, Flagged: true, DeviceID: "iphone-15"})
	if rec.Code != http.StatusOK {
		t.Fatalf("putEdit: %d %s", rec.Code, rec.Body.String())
	}

	rec = c.do(t, "POST", "/v1/images/"+imgID+"/assets/confirm",
		protocol.ConfirmAssetRequest{
			Tier: protocol.TierOriginal, StorageKey: "s3://b/o.nef", ByteSize: 55 << 20,
		})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("confirmAsset: %d %s", rec.Code, rec.Body.String())
	}

	// Đồng bộ tiếp: chỉ nhận thay đổi mới, không kéo lại từ đầu.
	rec = c.do(t, "GET", fmt.Sprintf("/v1/sessions/%s/changes?since=%d", sid, ch.Revision), nil)
	next := decodeInto[protocol.ChangesResponse](t, rec)
	if len(next.Edits) != 1 {
		t.Errorf("lấy %d edit, muốn 1", len(next.Edits))
	}
	if len(next.Images) != 1 {
		t.Errorf("lấy %d ảnh, muốn 1 (ảnh có asset mới)", len(next.Images))
	}
	if len(next.Images) == 1 {
		if _, ok := next.Images[0].Assets[protocol.TierOriginal]; !ok {
			t.Errorf("asset original không tới được client")
		}
	}
}

// TestEmptyArraysNotNull: client TypeScript sẽ vấp phải null nếu server trả
// `"images": null` thay vì `[]`. Lỗi nhỏ nhưng gây crash ở phía app.
func TestEmptyArraysNotNull(t *testing.T) {
	c := newAuthed(t)
	sid := mkSession(t, c)

	rec := c.do(t, "GET", "/v1/sessions/"+sid+"/changes?since=0", nil)
	body := rec.Body.String()
	if strings.Contains(body, `"images":null`) || strings.Contains(body, `"edits":null`) {
		t.Errorf("mảng rỗng bị serialize thành null: %s", body)
	}
	if !strings.Contains(body, `"images":[]`) {
		t.Errorf("muốn images:[] trong response: %s", body)
	}
}

// TestUnknownFieldRejected: gõ nhầm tên trường mà bị bỏ qua âm thầm sẽ tạo ảnh
// trùng lặp hàng loạt, và triệu chứng xuất hiện rất xa nguyên nhân.
//
// Dùng snake_case làm ca thử vì đó là nhầm lẫn thật sự hay xảy ra — lập trình
// viên client quen quy ước snake_case của backend khác.
func TestUnknownFieldRejected(t *testing.T) {
	c := newAuthed(t)
	sid := mkSession(t, c)

	rec := c.do(t, "POST", "/v1/sessions/"+sid+"/images/batch", map[string]any{
		"images": []map[string]any{{"client_id": "snake-case", "filename": "a.NEF"}},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, muốn 400 — trường lạ phải bị từ chối", rec.Code)
	}
}

// TestFieldMatchingIsCaseInsensitive ghi lại một hành vi CỦA GO dễ gây bất ngờ:
// encoding/json khớp tên trường không phân biệt hoa thường, nên `clientID`,
// `ClientId`, `CLIENTID` đều khớp thẻ `clientId` và KHÔNG bị
// DisallowUnknownFields chặn.
//
// Ghi lại bằng test thay vì bằng comment, vì đây là thứ người sau sẽ tưởng là
// lỗi khi thấy DisallowUnknownFields không bắt được một "typo" viết hoa.
func TestFieldMatchingIsCaseInsensitive(t *testing.T) {
	c := newAuthed(t)
	sid := mkSession(t, c)

	rec := c.do(t, "POST", "/v1/sessions/"+sid+"/images/batch", map[string]any{
		"images": []map[string]any{{"clientID": "hoa-thuong", "filename": "a.NEF"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, muốn 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if _, ok := decodeInto[protocol.BatchImagesResponse](t, rec).IDs["hoa-thuong"]; !ok {
		t.Error("clientID viết hoa phải khớp thẻ clientId")
	}
}

func TestValidation(t *testing.T) {
	c := newAuthed(t)
	sid := mkSession(t, c)

	res := c.do(t, "POST", "/v1/sessions/"+sid+"/images/batch",
		protocol.BatchImagesRequest{Images: []protocol.ImageInput{
			{ClientID: "c1", Filename: "a.NEF", Format: protocol.FormatNEF},
		}})
	imgID := decodeInto[protocol.BatchImagesResponse](t, res).IDs["c1"]

	cases := []struct {
		name   string
		method string
		path   string
		body   any
		want   int
	}{
		{"session không tồn tại", "GET", "/v1/sessions/khong-co/changes?since=0", nil, 404},
		{"ảnh không tồn tại", "PUT", "/v1/images/khong-co/edit", protocol.PutEditRequest{}, 404},
		{"since không phải số", "GET", "/v1/sessions/" + sid + "/changes?since=abc", nil, 400},
		{"rating ngoài khoảng", "PUT", "/v1/images/" + imgID + "/edit",
			protocol.PutEditRequest{Rating: 9}, 400},
		{"tier không hợp lệ", "POST", "/v1/images/" + imgID + "/assets/confirm",
			protocol.ConfirmAssetRequest{Tier: "linh-tinh", StorageKey: "k"}, 400},
		{"storageKey rỗng", "POST", "/v1/images/" + imgID + "/assets/confirm",
			protocol.ConfirmAssetRequest{Tier: protocol.TierThumb}, 400},
		{"session thiếu name", "POST", "/v1/sessions", map[string]any{}, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := c.do(t, tc.method, tc.path, tc.body)
			if rec.Code != tc.want {
				t.Errorf("code = %d, muốn %d (body=%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestInternalErrorsDoNotLeak: thông báo lỗi của tầng dưới hay lộ tên bảng và
// cấu trúc truy vấn. Client chỉ được thấy thông báo chung.
func TestInternalErrorsDoNotLeak(t *testing.T) {
	c := newAuthed(t)
	sid := mkSession(t, c)

	// clientId rỗng khiến store trả ErrConflict; kiểm tra nó ánh xạ đúng 409
	// và không kèm chi tiết nội bộ.
	rec := c.do(t, "POST", "/v1/sessions/"+sid+"/images/batch",
		protocol.BatchImagesRequest{Images: []protocol.ImageInput{{Filename: "a.NEF"}}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d, muốn 409 (body=%s)", rec.Code, rec.Body.String())
	}
	errResp := decodeInto[protocol.ErrorResponse](t, rec)
	if errResp.Code != protocol.ErrCodeConflict {
		t.Errorf("code = %q, muốn %q", errResp.Code, protocol.ErrCodeConflict)
	}
}

func TestBodyTooLarge(t *testing.T) {
	c := newAuthed(t)
	sid := mkSession(t, c)

	huge := strings.Repeat("x", maxBodyBytes+1024)
	req := httptest.NewRequest("POST", "/v1/sessions/"+sid+"/images/batch",
		strings.NewReader(`{"images":[{"clientId":"`+huge+`"}]}`))
	req.Header.Set("Authorization", "Bearer "+c.token)
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, muốn 400 — body quá lớn phải bị chặn", rec.Code)
	}
}

// TestEveryProtectedRouteRequiresAuth duyệt TỪNG route dữ liệu và khẳng định nó
// trả 401 khi không có token.
//
// Bảo vệ trước một lỗi rất dễ xảy ra và rất khó thấy khi review: quên bọc
// requireAuth ở một dòng đăng ký route. Code vẫn biên dịch, test chức năng vẫn
// xanh vì chúng luôn gửi token — chỉ có route đó là công khai với cả thế giới.
func TestEveryProtectedRouteRequiresAuth(t *testing.T) {
	h := newTestServer()

	routes := []struct {
		method string
		path   string
		body   any
	}{
		{"POST", "/v1/sessions", map[string]any{"name": "x"}},
		{"POST", "/v1/sessions/bat-ky/images/batch", protocol.BatchImagesRequest{}},
		{"GET", "/v1/sessions/bat-ky/changes?since=0", nil},
		{"PUT", "/v1/images/bat-ky/edit", protocol.PutEditRequest{}},
		{"POST", "/v1/images/bat-ky/assets/confirm", protocol.ConfirmAssetRequest{Tier: protocol.TierThumb, StorageKey: "k"}},
		{"DELETE", "/v1/images/bat-ky", nil},
		{"GET", "/v1/me", nil},
		{"POST", "/v1/auth/signout-everywhere", nil},
	}

	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := do(t, h, rt.method, rt.path, rt.body)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("KHÔNG YÊU CẦU XÁC THỰC: code = %d, muốn 401 (body=%s)",
					rec.Code, rec.Body.String())
			}
		})
	}
}

// TestCannotAccessOtherUsersData là test IDOR.
//
// Xác thực chỉ trả lời "anh là ai". Nó KHÔNG trả lời "anh được xem cái gì". Thiếu
// bước thứ hai thì bất kỳ người dùng nào đăng nhập hợp lệ cũng đọc được dữ liệu
// của mọi người khác chỉ bằng cách đoán id — và id thì thường lộ ra ở URL, log,
// hay ảnh chụp màn hình.
func TestCannotAccessOtherUsersData(t *testing.T) {
	h := newTestServer()
	victim := signUpAs(t, h, "nan-nhan@example.com")
	attacker := signUpAs(t, h, "ke-tan-cong@example.com")

	sid := mkSession(t, victim)
	res := victim.do(t, "POST", "/v1/sessions/"+sid+"/images/batch",
		protocol.BatchImagesRequest{Images: []protocol.ImageInput{
			{ClientID: "c1", Filename: "rieng-tu.NEF", Format: protocol.FormatNEF},
		}})
	if res.Code != http.StatusOK {
		t.Fatalf("nạn nhân đẩy ảnh: %d %s", res.Code, res.Body.String())
	}
	imgID := decodeInto[protocol.BatchImagesResponse](t, res).IDs["c1"]

	attempts := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"đọc thay đổi của buổi chụp người khác", "GET", "/v1/sessions/" + sid + "/changes?since=0", nil},
		{"đẩy ảnh vào buổi chụp người khác", "POST", "/v1/sessions/" + sid + "/images/batch",
			protocol.BatchImagesRequest{Images: []protocol.ImageInput{{ClientID: "x", Filename: "x.NEF"}}}},
		{"sửa ảnh người khác", "PUT", "/v1/images/" + imgID + "/edit", protocol.PutEditRequest{Rating: 1}},
		{"gắn asset vào ảnh người khác", "POST", "/v1/images/" + imgID + "/assets/confirm",
			protocol.ConfirmAssetRequest{Tier: protocol.TierThumb, StorageKey: "k"}},
		{"xoá ảnh người khác", "DELETE", "/v1/images/" + imgID, nil},
	}

	for _, a := range attempts {
		t.Run(a.name, func(t *testing.T) {
			rec := attacker.do(t, a.method, a.path, a.body)
			// Phải là 404 chứ KHÔNG phải 403: trả 403 là xác nhận "id này có tồn
			// tại, chỉ không phải của bạn", đủ để dò ra id hợp lệ.
			if rec.Code != http.StatusNotFound {
				t.Errorf("code = %d, muốn 404 (body=%s)", rec.Code, rec.Body.String())
			}
		})
	}

	// Và dữ liệu của nạn nhân phải còn nguyên vẹn sau mọi lần thử.
	rec := victim.do(t, "GET", "/v1/sessions/"+sid+"/changes?since=0", nil)
	ch := decodeInto[protocol.ChangesResponse](t, rec)
	if len(ch.Images) != 1 {
		t.Errorf("nạn nhân còn %d ảnh, muốn 1 — dữ liệu đã bị can thiệp", len(ch.Images))
	}
	if len(ch.Edits) != 0 {
		t.Errorf("có %d chỉnh sửa lạ trên dữ liệu nạn nhân", len(ch.Edits))
	}
}

// TestNonexistentAndForeignLookAlike: id không tồn tại và id của người khác phải
// cho ra phản hồi KHÔNG PHÂN BIỆT được. Khác nhau là biến API thành công cụ dò.
func TestNonexistentAndForeignLookAlike(t *testing.T) {
	h := newTestServer()
	victim := signUpAs(t, h, "a@example.com")
	attacker := signUpAs(t, h, "b@example.com")
	sid := mkSession(t, victim)

	foreign := attacker.do(t, "GET", "/v1/sessions/"+sid+"/changes?since=0", nil)
	missing := attacker.do(t, "GET", "/v1/sessions/khong-ton-tai-that/changes?since=0", nil)

	if foreign.Code != missing.Code || foreign.Body.String() != missing.Body.String() {
		t.Errorf("phân biệt được id có thật với id không tồn tại:\n  của người khác: %d %s\n  không tồn tại:  %d %s",
			foreign.Code, foreign.Body.String(), missing.Code, missing.Body.String())
	}
}

func TestAuthEndpoints(t *testing.T) {
	h := newTestServer()

	t.Run("đăng ký rồi lấy /v1/me", func(t *testing.T) {
		c := signUpAs(t, h, "me@example.com")
		rec := c.do(t, "GET", "/v1/me", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "me@example.com") {
			t.Errorf("/v1/me không trả email: %s", rec.Body.String())
		}
	})

	t.Run("đăng nhập sai không tiết lộ tài khoản tồn tại", func(t *testing.T) {
		wrongPass := do(t, h, "POST", "/v1/auth/signin",
			map[string]any{"email": "me@example.com", "password": "sai-mat-khau-dai"})
		noAccount := do(t, h, "POST", "/v1/auth/signin",
			map[string]any{"email": "chua-dang-ky@example.com", "password": "sai-mat-khau-dai"})

		if wrongPass.Code != noAccount.Code || wrongPass.Body.String() != noAccount.Body.String() {
			t.Errorf("phản hồi khác nhau:\n  sai mật khẩu: %d %s\n  chưa đăng ký: %d %s",
				wrongPass.Code, wrongPass.Body.String(), noAccount.Code, noAccount.Body.String())
		}
		if wrongPass.Code != http.StatusUnauthorized {
			t.Errorf("code = %d, muốn 401", wrongPass.Code)
		}
	})

	t.Run("đăng xuất vô hiệu hoá token", func(t *testing.T) {
		c := signUpAs(t, h, "signout@example.com")
		if rec := c.do(t, "POST", "/v1/auth/signout", nil); rec.Code != http.StatusNoContent {
			t.Fatalf("đăng xuất: %d", rec.Code)
		}
		if rec := c.do(t, "GET", "/v1/me", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("token sau đăng xuất vẫn dùng được: %d", rec.Code)
		}
	})

	t.Run("oidc thiếu nonce bị từ chối", func(t *testing.T) {
		rec := do(t, h, "POST", "/v1/auth/oidc",
			map[string]any{"provider": "google", "idToken": "gia.mao.token"})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("code = %d, muốn 400 — thiếu nonce phải bị chặn", rec.Code)
		}
	})

	t.Run("provider lạ bị từ chối", func(t *testing.T) {
		rec := do(t, h, "POST", "/v1/auth/oidc",
			map[string]any{"provider": "facebook", "idToken": "x", "nonce": "n"})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("code = %d, muốn 400", rec.Code)
		}
	})

	t.Run("token rác bị từ chối", func(t *testing.T) {
		for _, tok := range []string{"rac", "Bearer", strings.Repeat("A", 100)} {
			rec := doAuth(t, h, tok, "GET", "/v1/me", nil)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("token %q: code = %d, muốn 401", tok, rec.Code)
			}
		}
	})
}
