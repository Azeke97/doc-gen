package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"docgen/internal/db"
	"docgen/internal/templates"
)

func TestGetTemplate(t *testing.T) {
	s := newAdminTestServer(t)
	id := uuid.New()
	s.templates.tmpl = db.Template{ID: id, Project: "procurement", Code: "opening_protocol", Locale: "bilingual", Status: "active"}
	token := s.login()

	rec := s.do(http.MethodGet, "/admin/v1/templates/"+id.String(), "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var got templateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != id || got.Code != "opening_protocol" || s.templates.gotIDs[0] != id {
		t.Errorf("got %+v", got)
	}

	s.templates.err = templates.ErrTemplateNotFound
	if rec := s.do(http.MethodGet, "/admin/v1/templates/"+id.String(), "", token); rec.Code != http.StatusNotFound {
		t.Errorf("not found: status = %d", rec.Code)
	}
	if rec := s.do(http.MethodGet, "/admin/v1/templates/"+id.String(), "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: status = %d", rec.Code)
	}
}

func TestVersionFile(t *testing.T) {
	s := newAdminTestServer(t)
	s.templates.tmpl = db.Template{Code: "opening_protocol", Locale: "kk", Engine: "html"}
	s.templates.version = db.TemplateVersion{Version: 3}
	s.templates.file = []byte("<!doctype html><title>x</title>")
	token := s.login()
	versionID := uuid.New()

	rec := s.do(http.MethodGet, "/admin/v1/versions/"+versionID.String()+"/file", "", token)
	if rec.Code != http.StatusOK || rec.Body.String() != "<!doctype html><title>x</title>" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body)
	}
	wantHeaders := map[string]string{
		"Content-Type":           "application/octet-stream",
		"Content-Disposition":    `attachment; filename="opening_protocol-kk-v3.html"`,
		"X-Content-Type-Options": "nosniff",
	}
	for name, want := range wantHeaders {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	s.templates.err = templates.ErrVersionNotFound
	if rec := s.do(http.MethodGet, "/admin/v1/versions/"+versionID.String()+"/file", "", token); rec.Code != http.StatusNotFound {
		t.Errorf("not found: status = %d", rec.Code)
	}
}
