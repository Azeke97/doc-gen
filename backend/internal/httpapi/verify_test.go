package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"docgen/internal/db"
	"docgen/internal/documents"
)

const browserAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

func sampleVerified() db.GetDocumentByPublicIDRow {
	created := time.Date(2026, 10, 20, 7, 0, 12, 0, time.UTC)
	return db.GetDocumentByPublicIDRow{
		PublicID: "0195f3c9a1e54b7d8c2a8e6f1b3d5a7c", Format: "pdf", FileSha256: make([]byte, 32), FileSize: 9027,
		CreatedAt: created, ExpiresAt: created.Add(30 * 24 * time.Hour),
		TemplateName: `Протокол <вскрытия>`, ProjectName: "Портал закупок",
	}
}

// doVerify выполняет запрос без API-ключа, как сканер QR-кода.
func doVerify(t *testing.T, svc DocumentService, publicID, accept string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/verify/"+publicID, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	NewRouter(Deps{APIClients: &fakeClientStore{}, Documents: svc}).ServeHTTP(rec, req)
	return rec
}

func TestVerifyJSON(t *testing.T) {
	svc := &fakeDocumentService{verify: sampleVerified()}
	rec := doVerify(t, svc, "0195f3c9a1e54b7d8c2a8e6f1b3d5a7c", "application/json")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.gotPublic != "0195f3c9a1e54b7d8c2a8e6f1b3d5a7c" {
		t.Errorf("public id = %q", svc.gotPublic)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["valid"] != true || got["template_name"] != "Протокол <вскрытия>" || got["file_sha256"] != strings.Repeat("0", 64) {
		t.Errorf("body = %v", got)
	}
	if sigs, ok := got["signatures"].([]any); !ok || len(sigs) != 0 {
		t.Errorf("signatures = %v, want []", got["signatures"])
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("headers = %v", rec.Header())
	}
}

func TestVerifyJSONErrors(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{documents.ErrDocumentNotFound, http.StatusNotFound, "document_not_found"},
		{documents.ErrDocumentExpired, http.StatusGone, "document_expired"},
	}
	for _, tt := range tests {
		rec := doVerify(t, &fakeDocumentService{err: tt.err}, "0195f3c9a1e54b7d8c2a8e6f1b3d5a7c", "")
		if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.code) {
			t.Errorf("%v: status = %d, body = %s", tt.err, rec.Code, rec.Body)
		}
	}
}

func TestVerifyPage(t *testing.T) {
	rec := doVerify(t, &fakeDocumentService{verify: sampleVerified()}, "0195f3c9a1e54b7d8c2a8e6f1b3d5a7c", browserAccept)

	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("status = %d, content-type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Документ подлинный", "Құжат түпнұсқа", "Протокол &lt;вскрытия&gt;", "Портал закупок",
		"20.10.2026 12:00", // время Казахстана
		"0195f3c9a1e54b7d8c2a8e6f1b3d5a7c", "19.11.2026",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page has no %q", want)
		}
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Errorf("csp = %q", rec.Header().Get("Content-Security-Policy"))
	}
}

func TestVerifyPageErrors(t *testing.T) {
	tests := []struct {
		err    error
		status int
		want   string
	}{
		{documents.ErrDocumentNotFound, http.StatusNotFound, "Документ не найден"},
		{documents.ErrDocumentExpired, http.StatusGone, "Срок хранения документа истёк"},
	}
	for _, tt := range tests {
		rec := doVerify(t, &fakeDocumentService{err: tt.err}, "x", browserAccept)
		if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.want) {
			t.Errorf("%v: status = %d, body = %s", tt.err, rec.Code, rec.Body)
		}
	}
}

// Публичный маршрут не должен снять проверку ключа с остальных маршрутов /v1.
func TestVerifyRouteKeepsKeyCheck(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/whoami", nil)
	rec := httptest.NewRecorder()
	NewRouter(Deps{APIClients: &fakeClientStore{}, Documents: &fakeDocumentService{}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("whoami without key: status = %d", rec.Code)
	}
}
