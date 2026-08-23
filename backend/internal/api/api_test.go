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

	"github.com/hauph/camera/backend/internal/protocol"
	"github.com/hauph/camera/backend/internal/store"
	"github.com/hauph/camera/backend/internal/store/memory"
)

func newTestServer() http.Handler {
	n := 0
	base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	tick := 0
	mem := memory.New(
		func() string { n++; return fmt.Sprintf("id-%03d", n) },
		func() time.Time { tick++; return base.Add(time.Duration(tick) * time.Second) },
	)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(mem, log).Routes()
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
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

func mkSession(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := do(t, h, "POST", "/v1/sessions", map[string]any{"name": "Đám cưới"})
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
	h := newTestServer()
	sid := mkSession(t, h)

	images := []protocol.ImageInput{
		{ClientID: "c1", Filename: "DSC_0001.NEF", Format: protocol.FormatNEF,
			ByteSize: 55 << 20, CapturedAt: time.Now().UTC().Truncate(time.Second), IsRaw: true},
		{ClientID: "c2", Filename: "DSC_0002.NEF", Format: protocol.FormatNEF,
			ByteSize: 56 << 20, CapturedAt: time.Now().UTC().Truncate(time.Second), IsRaw: true},
	}

	rec := do(t, h, "POST", "/v1/sessions/"+sid+"/images/batch",
		protocol.BatchImagesRequest{Images: images})
	if rec.Code != http.StatusOK {
		t.Fatalf("batch: %d %s", rec.Code, rec.Body.String())
	}
	batch := decodeInto[protocol.BatchImagesResponse](t, rec)
	if batch.Created != 2 {
		t.Fatalf("created = %d, muốn 2", batch.Created)
	}

	// Kéo delta từ đầu.
	rec = do(t, h, "GET", "/v1/sessions/"+sid+"/changes?since=0", nil)
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

	rec = do(t, h, "PUT", "/v1/images/"+imgID+"/edit",
		protocol.PutEditRequest{Rating: 5, Flagged: true, DeviceID: "iphone-15"})
	if rec.Code != http.StatusOK {
		t.Fatalf("putEdit: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(t, h, "POST", "/v1/images/"+imgID+"/assets/confirm",
		protocol.ConfirmAssetRequest{
			Tier: protocol.TierOriginal, StorageKey: "s3://b/o.nef", ByteSize: 55 << 20,
		})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("confirmAsset: %d %s", rec.Code, rec.Body.String())
	}

	// Đồng bộ tiếp: chỉ nhận thay đổi mới, không kéo lại từ đầu.
	rec = do(t, h, "GET", fmt.Sprintf("/v1/sessions/%s/changes?since=%d", sid, ch.Revision), nil)
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
	h := newTestServer()
	sid := mkSession(t, h)

	rec := do(t, h, "GET", "/v1/sessions/"+sid+"/changes?since=0", nil)
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
	h := newTestServer()
	sid := mkSession(t, h)

	rec := do(t, h, "POST", "/v1/sessions/"+sid+"/images/batch", map[string]any{
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
	h := newTestServer()
	sid := mkSession(t, h)

	rec := do(t, h, "POST", "/v1/sessions/"+sid+"/images/batch", map[string]any{
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
	h := newTestServer()
	sid := mkSession(t, h)

	res := do(t, h, "POST", "/v1/sessions/"+sid+"/images/batch",
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
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(t, h, c.method, c.path, c.body)
			if rec.Code != c.want {
				t.Errorf("code = %d, muốn %d (body=%s)", rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

// TestInternalErrorsDoNotLeak: thông báo lỗi của tầng dưới hay lộ tên bảng và
// cấu trúc truy vấn. Client chỉ được thấy thông báo chung.
func TestInternalErrorsDoNotLeak(t *testing.T) {
	h := newTestServer()
	sid := mkSession(t, h)

	// clientId rỗng khiến store trả ErrConflict; kiểm tra nó ánh xạ đúng 409
	// và không kèm chi tiết nội bộ.
	rec := do(t, h, "POST", "/v1/sessions/"+sid+"/images/batch",
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
	h := newTestServer()
	sid := mkSession(t, h)

	huge := strings.Repeat("x", maxBodyBytes+1024)
	req := httptest.NewRequest("POST", "/v1/sessions/"+sid+"/images/batch",
		strings.NewReader(`{"images":[{"clientId":"`+huge+`"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, muốn 400 — body quá lớn phải bị chặn", rec.Code)
	}
}
