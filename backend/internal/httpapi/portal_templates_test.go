package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"docgen/internal/apikey"
	"docgen/internal/db"
	"docgen/internal/templates"
)

// fakePortalService запоминает, с чем его вызвали, и возвращает заданный результат.
type fakePortalService struct {
	rows       []db.ListActiveTemplatesByProjectRow
	resolved   db.ResolveTemplateVersionRow
	err        error
	gotProject string
	gotRef     templates.Ref
}

func (f *fakePortalService) ListActive(_ context.Context, project string) ([]db.ListActiveTemplatesByProjectRow, error) {
	f.gotProject = project
	return f.rows, f.err
}

func (f *fakePortalService) Resolve(_ context.Context, project string, ref templates.Ref) (db.ResolveTemplateVersionRow, error) {
	f.gotProject, f.gotRef = project, ref
	return f.resolved, f.err
}

// doPortalTemplates вызывает GET /v1/templates с ключом портала проекта procurement.
func doPortalTemplates(t *testing.T, store *fakePortalService, path string) *httptest.ResponseRecorder {
	t.Helper()

	key := apikey.Generate()
	clients := &fakeClientStore{clients: map[string]db.APIClient{
		string(apikey.Hash(key)): {Name: "Портал закупок", Project: "procurement"},
	}}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	req.Header.Set(apiKeyHeader, key)

	rec := httptest.NewRecorder()
	NewRouter(Deps{APIClients: clients, PortalTemplates: store}).ServeHTTP(rec, req)

	return rec
}

func TestPortalTemplates(t *testing.T) {
	store := &fakePortalService{rows: []db.ListActiveTemplatesByProjectRow{
		{Code: "opening_protocol", Locale: "bilingual", Name: "Протокол вскрытия", Engine: "html", ActiveVersion: 2},
	}}

	// Чужой проект в query игнорируется: проект берётся только из ключа.
	rec := doPortalTemplates(t, store, "/v1/templates?project=auctions")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if store.gotProject != "procurement" {
		t.Errorf("project = %q, want procurement (from API key)", store.gotProject)
	}

	var body []portalTemplateResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	want := []portalTemplateResponse{
		{Code: "opening_protocol", Locale: "bilingual", Name: "Протокол вскрытия", Engine: "html", ActiveVersion: 2},
	}
	if !slices.Equal(body, want) {
		t.Errorf("body = %+v, want %+v", body, want)
	}
}

func TestPortalTemplatesEmpty(t *testing.T) {
	rec := doPortalTemplates(t, &fakePortalService{}, "/v1/templates")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	// Пустой каталог — пустой массив, а не null: порталу не нужно отдельно проверять null.
	if got := rec.Body.String(); got != "[]\n" {
		t.Errorf("body = %q, want []", got)
	}
}

func TestPortalTemplatesStoreFailure(t *testing.T) {
	rec := doPortalTemplates(t, &fakePortalService{err: errors.New("connection refused")}, "/v1/templates")

	assertErrorCode(t, rec, http.StatusInternalServerError, "internal")
}

func TestPortalTemplatesRequireKey(t *testing.T) {
	rec := doRequest(t, &fakeClientStore{}, "/v1/templates", "")

	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

func TestPortalSchema(t *testing.T) {
	svc := &fakePortalService{resolved: db.ResolveTemplateVersionRow{
		Code: "opening_protocol", Locale: "bilingual", Version: 3, DataSchema: json.RawMessage(`{"type":"object"}`),
	}}

	rec := doPortalTemplates(t, svc, "/v1/templates/opening_protocol/schema?locale=bilingual&version=3")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	want := templates.Ref{Code: "opening_protocol", Locale: "bilingual", Version: 3}
	if svc.gotRef != want || svc.gotProject != "procurement" {
		t.Errorf("Resolve(%q, %+v), want (procurement, %+v)", svc.gotProject, svc.gotRef, want)
	}
	if got := rec.Body.String(); got != `{"code":"opening_protocol","locale":"bilingual","version":3,"schema":{"type":"object"}}`+"\n" {
		t.Errorf("body = %s", got)
	}
}

func TestPortalSchemaErrors(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		svcErr     error
		wantStatus int
		wantCode   string
	}{
		{name: "bad version", path: "/v1/templates/x/schema?version=abc", wantStatus: http.StatusBadRequest, wantCode: "invalid_version"},
		{name: "zero version", path: "/v1/templates/x/schema?version=0", wantStatus: http.StatusBadRequest, wantCode: "invalid_version"},
		{
			name:       "several locales",
			path:       "/v1/templates/x/schema",
			svcErr:     &templates.ValidationError{Field: "locale", Message: "choose one of: kk, ru"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_locale",
		},
		{name: "unknown template", path: "/v1/templates/x/schema", svcErr: templates.ErrTemplateNotFound, wantStatus: http.StatusNotFound, wantCode: "template_not_found"},
		{name: "unknown version", path: "/v1/templates/x/schema?version=9", svcErr: templates.ErrVersionNotFound, wantStatus: http.StatusNotFound, wantCode: "version_not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doPortalTemplates(t, &fakePortalService{err: tt.svcErr}, tt.path)
			assertErrorCode(t, rec, tt.wantStatus, tt.wantCode)
		})
	}
}
