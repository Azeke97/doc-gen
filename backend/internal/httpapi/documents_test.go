package httpapi

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"docgen/internal/apikey"
	"docgen/internal/db"
	"docgen/internal/documents"
	"docgen/internal/docx"
	"docgen/internal/gotenberg"
)

// fakeDocumentService запоминает запрос и возвращает заданный результат.
type fakeDocumentService struct {
	err     error
	created bool
	doc     db.GetProjectDocumentRow
	file    []byte
	verify  db.GetDocumentByPublicIDRow
	vars    []docx.Variable

	gotClient   db.APIClient
	gotReq      documents.DocxRequest
	gotTemplate []byte
	gotProject  string
	gotID       uuid.UUID
	gotPublic   string
}

func (f *fakeDocumentService) GenerateDOCX(_ context.Context, client db.APIClient, req documents.DocxRequest, _ string) (documents.Generated, error) {
	f.gotClient, f.gotReq = client, req
	if f.err != nil {
		return documents.Generated{}, f.err
	}
	return documents.Generated{Document: f.doc.Document, File: f.file, Created: f.created}, nil
}

func (f *fakeDocumentService) CheckTemplate(file []byte) ([]docx.Variable, error) {
	f.gotTemplate = file
	return f.vars, f.err
}

func (f *fakeDocumentService) Get(_ context.Context, project string, id uuid.UUID) (db.GetProjectDocumentRow, error) {
	f.gotProject, f.gotID = project, id
	return f.doc, f.err
}

func (f *fakeDocumentService) File(_ context.Context, project string, id uuid.UUID) (db.GetProjectDocumentRow, []byte, error) {
	f.gotProject, f.gotID = project, id
	return f.doc, f.file, f.err
}

func (f *fakeDocumentService) Verify(_ context.Context, publicID string) (db.GetDocumentByPublicIDRow, error) {
	f.gotPublic = publicID
	return f.verify, f.err
}

// fakeJournal запоминает записи журнала.
type fakeJournal struct {
	entries []db.CreateGenerationLogParams
	err     error
}

func (f *fakeJournal) CreateGenerationLog(_ context.Context, arg db.CreateGenerationLogParams) error {
	f.entries = append(f.entries, arg)
	return f.err
}

func sampleDocument() db.GetProjectDocumentRow {
	now := time.Date(2026, 10, 20, 7, 0, 12, 0, time.UTC)
	name, ext, by := "opening_protocol", "lot:209092", "user-17"
	return db.GetProjectDocumentRow{
		Document: db.Document{
			ID: uuid.New(), PublicID: "0195f3c9a1e54b7d8c2a8e6f1b3d5a7c", Project: "procurement", Format: "pdf",
			TemplateName: &name, TemplateSha256: make([]byte, 32), ExternalID: &ext, RequestedBy: &by,
			FileSha256: make([]byte, 32), FileSize: 12, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
		},
		DisplayTemplateName: name,
	}
}

type portalDeps struct {
	svc     DocumentService
	journal JournalStore
}

// doPortal выполняет запрос портала проекта procurement.
func doPortal(t *testing.T, deps portalDeps, method, path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	key := apikey.Generate()
	clients := &fakeClientStore{clients: map[string]db.APIClient{
		string(apikey.Hash(key)): {ID: uuid.New(), Name: "Портал закупок", Project: "procurement"},
	}}

	req := httptest.NewRequestWithContext(t.Context(), method, path, body)
	req.Header.Set(apiKeyHeader, key)
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	rec := httptest.NewRecorder()
	NewRouter(Deps{APIClients: clients, Documents: deps.svc, Journal: deps.journal}).ServeHTTP(rec, req)
	return rec
}

// generateForm собирает multipart-запрос на генерацию; поле со значением "" не отправляется.
func generateForm(t *testing.T, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	base := map[string]string{
		"template":       "PK docx bytes",
		"data":           `{"number":"0001"}`,
		"format":         "pdf",
		"template_name":  "opening_protocol",
		"external_id":    "lot:209092",
		"requested_by":   "user-17",
		"retention_days": "30",
	}
	for k, v := range fields {
		base[k] = v
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, value := range base {
		if value == "" {
			continue
		}
		var err error
		if name == "template" {
			var fw io.Writer
			if fw, err = mw.CreateFormFile(name, "protocol.docx"); err == nil {
				_, err = io.WriteString(fw, value)
			}
		} else {
			err = mw.WriteField(name, value)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func postGenerate(t *testing.T, svc *fakeDocumentService, journal *fakeJournal, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := generateForm(t, fields)
	return doPortal(t, portalDeps{svc: svc, journal: journal}, http.MethodPost, "/v1/documents", body,
		map[string]string{"Content-Type": ct, "Idempotency-Key": "key-1", "X-Request-Id": "req-1"})
}

func TestGenerateDocument(t *testing.T) {
	for _, created := range []bool{true, false} {
		t.Run(fmt.Sprintf("created=%v", created), func(t *testing.T) {
			svc := &fakeDocumentService{doc: sampleDocument(), file: []byte("%PDF-1.7 doc"), created: created}
			journal := &fakeJournal{}

			rec := postGenerate(t, svc, journal, nil)

			want, outcome := http.StatusOK, "repeated"
			if created {
				want, outcome = http.StatusCreated, "created"
			}
			if rec.Code != want || rec.Body.String() != "%PDF-1.7 doc" {
				t.Fatalf("status = %d, want %d, body = %q", rec.Code, want, rec.Body.String())
			}
			doc := svc.doc.Document
			wantHeaders := map[string]string{
				"Content-Type":          "application/pdf",
				"Content-Disposition":   `attachment; filename="0195f3c9a1e54b7d8c2a8e6f1b3d5a7c.pdf"`,
				"X-Document-Id":         doc.ID.String(),
				"X-Document-SHA256":     strings.Repeat("0", 64),
				"X-Document-Expires-At": "2026-11-19T07:00:12Z",
				"Location":              "/v1/documents/" + doc.ID.String(),
			}
			for name, v := range wantHeaders {
				if got := rec.Header().Get(name); got != v {
					t.Errorf("%s = %q, want %q", name, got, v)
				}
			}

			r := svc.gotReq
			if string(r.Template) != "PK docx bytes" || string(r.Data) != `{"number":"0001"}` || r.Format != "pdf" ||
				r.TemplateName != "opening_protocol" || r.ExternalID != "lot:209092" || r.RequestedBy != "user-17" ||
				r.RetentionDays != 30 || r.IdempotencyKey != "key-1" || svc.gotClient.Project != "procurement" {
				t.Errorf("request = %+v", r)
			}

			if len(journal.entries) != 1 {
				t.Fatalf("journal entries = %d", len(journal.entries))
			}
			e := journal.entries[0]
			if e.Status != outcome || int(e.HTTPStatus) != want || *e.DocumentID != doc.ID || *e.TemplateName != "opening_protocol" ||
				hex.EncodeToString(e.TemplateSha256) != hex.EncodeToString(sha256Sum([]byte("PK docx bytes"))) ||
				*e.ResultSize != 12 || *e.IdempotencyKey != "key-1" || e.RequestID != "req-1" || e.ErrorCode != nil {
				t.Errorf("journal entry = %+v", e)
			}
		})
	}
}

func TestGenerateDocumentErrors(t *testing.T) {
	tests := []struct {
		name       string
		fields     map[string]string
		err        error
		wantStatus int
		wantCode   string
		wantLog    string
	}{
		{name: "invalid field", err: &documents.ValidationError{Field: "retention_days", Message: "too long"}, wantStatus: 400, wantCode: "invalid_retention_days", wantLog: "rejected"},
		{name: "template error", err: &docx.TemplateError{Problems: []docx.Problem{{Message: "не закрыт"}}}, wantStatus: 422, wantCode: "invalid_template", wantLog: "rejected"},
		{name: "data error", err: &docx.DataError{Fields: []docx.FieldError{{Field: "lots[1].name", Message: "нет в данных"}}}, wantStatus: 400, wantCode: "invalid_data", wantLog: "rejected"},
		{name: "idempotency", err: documents.ErrIdempotencyConflict, wantStatus: 409, wantCode: "idempotency_conflict", wantLog: "rejected"},
		{name: "convert", err: fmt.Errorf("%w: bad", gotenberg.ErrConvert), wantStatus: 422, wantCode: "conversion_failed", wantLog: "rejected"},
		{name: "converter down", err: fmt.Errorf("%w: refused", gotenberg.ErrUnavailable), wantStatus: 503, wantCode: "converter_unavailable", wantLog: "failed"},
		{name: "internal", err: errors.New("disk full"), wantStatus: 500, wantCode: "internal", wantLog: "failed"},
		{name: "retention not a number", fields: map[string]string{"retention_days": "month"}, wantStatus: 400, wantCode: "invalid_retention_days", wantLog: "rejected"},
		{name: "unknown field", fields: map[string]string{"extra": "1"}, wantStatus: 400, wantCode: "invalid_form", wantLog: "rejected"},
		{name: "data too large", fields: map[string]string{"data": strings.Repeat("x", documents.MaxData+1)}, wantStatus: 413, wantCode: "request_too_large", wantLog: "rejected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			journal := &fakeJournal{}
			rec := postGenerate(t, &fakeDocumentService{err: tt.err}, journal, tt.fields)

			assertErrorCode(t, rec, tt.wantStatus, tt.wantCode)
			if len(journal.entries) != 1 {
				t.Fatalf("journal entries = %d", len(journal.entries))
			}
			e := journal.entries[0]
			if e.Status != tt.wantLog || int(e.HTTPStatus) != tt.wantStatus || *e.ErrorCode != tt.wantCode || e.DocumentID != nil {
				t.Errorf("journal entry = %+v", e)
			}
		})
	}
}

func TestGenerateErrorDetails(t *testing.T) {
	journal := &fakeJournal{}
	rec := postGenerate(t, &fakeDocumentService{err: &docx.DataError{Fields: []docx.FieldError{{Field: "lots[1].name", Message: "нет в данных"}}}}, journal, nil)

	var body struct {
		Error struct {
			Details []docx.FieldError `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "lots[1].name" {
		t.Errorf("details = %+v", body.Error.Details)
	}
	if !strings.Contains(string(journal.entries[0].ErrorDetails), "lots[1].name") {
		t.Errorf("journal details = %s", journal.entries[0].ErrorDetails)
	}
}

func TestGenerateNotMultipart(t *testing.T) {
	journal := &fakeJournal{}
	rec := doPortal(t, portalDeps{svc: &fakeDocumentService{}, journal: journal}, http.MethodPost, "/v1/documents",
		strings.NewReader(`{"template":"x"}`), map[string]string{"Content-Type": "application/json", "Idempotency-Key": "k"})
	assertErrorCode(t, rec, http.StatusBadRequest, "invalid_form")
	if len(journal.entries) != 1 || journal.entries[0].Status != "rejected" {
		t.Errorf("journal = %+v", journal.entries)
	}
}

// Сбой журнала не мешает выдать документ.
func TestGenerateJournalFailure(t *testing.T) {
	svc := &fakeDocumentService{doc: sampleDocument(), file: []byte("%PDF"), created: true}
	rec := postGenerate(t, svc, &fakeJournal{err: errors.New("db down")}, nil)
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestCheckTemplate(t *testing.T) {
	svc := &fakeDocumentService{vars: []docx.Variable{{Path: "lots[].name", Usage: []string{"value"}}}}
	body, ct := generateForm(t, map[string]string{"data": "", "format": "", "template_name": "", "external_id": "", "requested_by": "", "retention_days": ""})

	rec := doPortal(t, portalDeps{svc: svc}, http.MethodPost, "/v1/templates/check", body, map[string]string{"Content-Type": ct})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"path":"lots[].name"`) || !strings.Contains(rec.Body.String(), `"valid":true`) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if string(svc.gotTemplate) != "PK docx bytes" {
		t.Errorf("template = %q", svc.gotTemplate)
	}

	svc.err = &docx.TemplateError{Problems: []docx.Problem{{Part: "основной текст", Message: "не закрыт"}}}
	body, ct = generateForm(t, map[string]string{"data": "", "format": "", "template_name": "", "external_id": "", "requested_by": "", "retention_days": ""})
	rec = doPortal(t, portalDeps{svc: svc}, http.MethodPost, "/v1/templates/check", body, map[string]string{"Content-Type": ct})
	assertErrorCode(t, rec, http.StatusUnprocessableEntity, "invalid_template")
}

func TestGetDocument(t *testing.T) {
	svc := &fakeDocumentService{doc: sampleDocument()}
	id := svc.doc.Document.ID

	rec := doPortal(t, portalDeps{svc: svc}, http.MethodGet, "/v1/documents/"+id.String(), nil, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	var body documentResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ID != id || body.TemplateName != "opening_protocol" || body.TemplateSHA256 == nil || *body.ExternalID != "lot:209092" || svc.gotProject != "procurement" {
		t.Errorf("body = %+v", body)
	}
}

func TestDocumentFile(t *testing.T) {
	svc := &fakeDocumentService{doc: sampleDocument(), file: []byte("PK docx")}
	svc.doc.Document.Format = "docx"

	rec := doPortal(t, portalDeps{svc: svc}, http.MethodGet, "/v1/documents/"+svc.doc.Document.ID.String()+"/file", nil, nil)

	if rec.Code != http.StatusOK || rec.Body.String() != "PK docx" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	wantHeaders := map[string]string{
		"Content-Type":           documents.ContentTypeDOCX,
		"Content-Disposition":    `attachment; filename="0195f3c9a1e54b7d8c2a8e6f1b3d5a7c.docx"`,
		"Content-Length":         "7",
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
		{name: "integrity failure", path: "/v1/documents/" + uuid.NewString() + "/file", err: errors.New("sha256 mismatch"), wantStatus: http.StatusInternalServerError, wantCode: "internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doPortal(t, portalDeps{svc: &fakeDocumentService{err: tt.err}}, http.MethodGet, tt.path, nil, nil)
			assertErrorCode(t, rec, tt.wantStatus, tt.wantCode)
		})
	}
}
