package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminUI(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><div id=app></div>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log(1)"), 0o600); err != nil {
		t.Fatal(err)
	}

	router := NewRouter(Deps{AdminAuth: newFakeAdminAuthStore(t, "admin@example.com", "secret-password"), AdminUI: AdminUI(dir)})
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rec := get("/admin/")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `id=app`) {
		t.Fatalf("/admin/: status = %d, body = %q", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "script-src 'self'") || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("/admin/ headers = %v", rec.Header())
	}

	if rec := get("/admin/assets/app.js"); rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset: status = %d, headers = %v", rec.Code, rec.Header())
	}
	if rec := get("/admin"); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/admin/" {
		t.Errorf("/admin: status = %d, location = %q", rec.Code, rec.Header().Get("Location"))
	}

	// API админки по-прежнему отвечает JSON, а не файлами.
	if rec := get("/admin/v1/me"); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "unauthorized") {
		t.Errorf("/admin/v1/me: status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := get("/admin/v1/unknown"); rec.Code != http.StatusNotFound {
		t.Errorf("/admin/v1/unknown: status = %d", rec.Code)
	}
}

func TestAdminUIDisabled(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()
	NewRouter(Deps{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
