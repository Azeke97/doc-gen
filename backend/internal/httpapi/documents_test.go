package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"docgen/internal/apikey"
	"docgen/internal/dataschema"
	"docgen/internal/db"
	"docgen/internal/documents"
	"docgen/internal/templates"
)

// fakeDocumentService запоминает запрос и возвращает заданный результат.
type fakeDocumentService struct {
	err     error
	created bool
	doc     db.GetProjectDocumentRow
	file    []byte

	gotClient  db.APIClient
	gotReq     documents.GenerateRequest
	gotProject string
	gotID      uuid.UUID
}

func (f *fakeDocumentService) Generate(_ context.Context, client db.APIClient, req documents.GenerateRequest, _ string) (db.Document, bool, error) {
	f.gotClient, f.gotReq = client, req
	return f.doc.Document, f.created, f.err
}

func (f *fakeDocumentService) Get(_ context.Context, project string, id uuid.UUID) (db.GetProjectDocumentRow, error) {
	f.gotProject, f.gotID = project, id
	return f.doc, f.err
}

func (f *fakeDocumentService) File(_ context.Context, project string, id uuid.UUID) (db.GetProjectDocumentRow, []byte, error) {
	f.gotProject, f.gotID = project, id
	return f.doc, f.file, f.err
}

func sampleDocument() db.GetProjectDocumentRow {
	now := time.Date(2026, 10, 20, 7, 0, 12, 0, time.UTC)
	return db.GetProjectDocumentRow{
		Document: db.Document{
			ID: uuid.New(), PublicID: "0195f3c9a1e54b7d8c2a8e6f1b3d5a7c", Project: "procurement", Format: "pdf",
			FileSha256: make([]byte, 32), FileSize: 9027, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
		},
		TemplateCode: "opening_protocol", Locale: "bilingual", Version: 2,
	}
}

// doPortal выполняет запрос портала проекта procurement.
func doPortal(t *testing.T, svc DocumentService, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	key := apikey.Generate()
	clients := &fakeClientStore{clients: map[string]db.APIClient{
		string(apikey.Hash(key)): {ID: uuid.New(), Name: "Портал закупок", Project: "procurement"},
	}}

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set(apiKeyHeader, key)
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	rec := httptest.NewRecorder()
	NewRouter(Deps{APIClients: clients, Documents: svc}).ServeHTTP(rec, req)
	return rec
}

const generateBody = `{"template":"opening_protocol","locale":"bilingual","data":{"number":"0001"},"external_id":"lot-1"}`

func TestGenerateDocument(t *testing.T) {
	for _, created := range []bool{true, false} {
		t.Run(fmt.Sprintf("created=%v", created), func(t *testing.T) {
			svc := &fakeDocumentService{doc: sampleDocument(), created: created}

			rec := doPortal(t, svc, http.MethodPost, "/v1/documents", generateBody, map[string]string{"Idempotency-Key": "key-1"})

			want := http.StatusOK
			if created {
				want = http.StatusCreated
			}
			if rec.Code != want {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
			}
			if got := rec.Header().Get("Location"); got != "/v1/documents/"+svc.doc.Document.ID.String() {
				t.Errorf("Location = %q", got)
			}
			if svc.gotReq.IdempotencyKey != "key-1" || svc.gotReq.Template != "opening_protocol" ||
				string(svc.gotReq.Data) != `{"number":"0001"}` || *svc.gotReq.ExternalID != "lot-1" || svc.gotClient.Project != "procurement" {
				t.Errorf("request = %+v, client %+v", svc.gotReq, svc.gotClient)
			}

			var body documentResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.PublicID != svc.doc.Document.PublicID || body.Template != "opening_protocol" || body.Version != 2 || len(body.FileSHA256) != 64 {
				t.Errorf("body = %+v", body)
			}
		})
	}
}

func TestGenerateDocumentErrors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "idempotency conflict", err: documents.ErrIdempotencyConflict, wantStatus: http.StatusConflict, wantCode: "idempotency_conflict"},
		{
			name:       "invalid data",
			err:        &templates.ValidationError{Field: "data", Message: "does not match", Errors: []dataschema.FieldError{{Field: "/number", Message: "is required"}}},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_data",
		},
		{name: "unknown template", err: templates.ErrTemplateNotFound, wantStatus: http.StatusNotFound, wantCode: "template_not_found"},
		{name: "render failed", err: fmt.Errorf("%w: boom", templates.ErrRenderFailed), wantStatus: http.StatusUnprocessableEntity, wantCode: "render_failed"},
		{name: "unknown field", body: `{"template":"x","data":{},"extra":1}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
		{name: "too large", body: `{"template":"x","data":{"s":"` + strings.Repeat("x", maxDocumentRequest) + `"}}`, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "request_too_large"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := tt.body
			if body == "" {
				body = generateBody
			}

			rec := doPortal(t, &fakeDocumentService{err: tt.err}, http.MethodPost, "/v1/documents", body, map[string]string{"Idempotency-Key": "k"})

			assertErrorCode(t, rec, tt.wantStatus, tt.wantCode)
		})
	}
}

func TestGetDocument(t *testing.T) {
	svc := &fakeDocumentService{doc: sampleDocument()}
	id := svc.doc.Document.ID

	rec := doPortal(t, svc, http.MethodGet, "/v1/documents/"+id.String(), "", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if svc.gotProject != "procurement" || svc.gotID != id {
		t.Errorf("Get(%q, %s), want procurement and %s", svc.gotProject, svc.gotID, id)
	}
}

func TestDocumentFile(t *testing.T) {
	svc := &fakeDocumentService{doc: sampleDocument(), file: []byte("%PDF-1.7 doc")}

	rec := doPortal(t, svc, http.MethodGet, "/v1/documents/"+svc.doc.Document.ID.String()+"/file", "", nil)

	if rec.Code != http.StatusOK || rec.Body.String() != "%PDF-1.7 doc" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	wantHeaders := map[string]string{
		"Content-Type":           "application/pdf",
		"Content-Disposition":    `attachment; filename="opening_protocol-0195f3c9a1e54b7d8c2a8e6f1b3d5a7c.pdf"`,
		"Content-Length":         "12",
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
	}
	for name, want := range wantHeaders {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestDocumentReadErrors(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "not found", path: "/v1/documents/" + uuid.NewString(), err: documents.ErrDocumentNotFound, wantStatus: http.StatusNotFound, wantCode: "document_not_found"},
		{name: "expired file", path: "/v1/documents/" + uuid.NewString() + "/file", err: documents.ErrDocumentExpired, wantStatus: http.StatusGone, wantCode: "document_expired"},
		{name: "bad id", path: "/v1/documents/nope", wantStatus: http.StatusBadRequest, wantCode: "invalid_id"},
		{name: "integrity failure", path: "/v1/documents/" + uuid.NewString() + "/file", err: fmt.Errorf("sha256 mismatch"), wantStatus: http.StatusInternalServerError, wantCode: "internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doPortal(t, &fakeDocumentService{err: tt.err}, http.MethodGet, tt.path, "", nil)
			assertErrorCode(t, rec, tt.wantStatus, tt.wantCode)
		})
	}
}

func TestDocumentsRequireKey(t *testing.T) {
	rec := doRequest(t, &fakeClientStore{}, "/v1/documents/"+uuid.NewString(), "")
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}
