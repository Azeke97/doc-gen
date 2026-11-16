package gotenberg

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeGotenberg запоминает последний запрос и отвечает заданным статусом и телом.
type fakeGotenberg struct {
	status int
	body   string

	path   string
	trace  string
	files  map[string]string
	fields map[string]string
}

func (f *fakeGotenberg) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.path, f.trace = r.URL.Path, r.Header.Get("Gotenberg-Trace")

	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil { //nolint:gosec // размер ограничен MaxBytesReader строкой выше
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.files = map[string]string{}
	for _, fh := range r.MultipartForm.File["files"] {
		file, _ := fh.Open()
		content, _ := io.ReadAll(file)
		_ = file.Close()
		f.files[fh.Filename] = string(content)
	}
	f.fields = map[string]string{}
	for name, values := range r.MultipartForm.Value {
		f.fields[name] = values[0]
	}

	w.WriteHeader(f.status)
	_, _ = io.WriteString(w, f.body)
}

func newFake(t *testing.T, status int, body string) (*fakeGotenberg, *Client) {
	t.Helper()

	fake := &fakeGotenberg{status: status, body: body}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	return fake, New(srv.URL+"/", 5*time.Second)
}

func TestConvertHTML(t *testing.T) {
	fake, c := newFake(t, http.StatusOK, "%PDF-1.4 fake")

	pdf, err := c.ConvertHTML(t.Context(), Document{
		HTML:   []byte("<p>Протокол</p>"),
		Footer: []byte(`<span class="pageNumber"></span>`),
		Assets: map[string][]byte{"logo.png": []byte("png")},
	}, "req-1")
	if err != nil {
		t.Fatalf("ConvertHTML: %v", err)
	}

	if string(pdf) != "%PDF-1.4 fake" {
		t.Errorf("pdf = %q", pdf)
	}
	if fake.path != "/forms/chromium/convert/html" || fake.trace != "req-1" {
		t.Errorf("path = %q, trace = %q", fake.path, fake.trace)
	}
	wantFiles := map[string]string{
		"index.html":  "<p>Протокол</p>",
		"footer.html": `<span class="pageNumber"></span>`,
		"logo.png":    "png",
	}
	if len(fake.files) != len(wantFiles) {
		t.Errorf("files = %v, want %v", fake.files, wantFiles)
	}
	for name, want := range wantFiles {
		if fake.files[name] != want {
			t.Errorf("file %s = %q, want %q", name, fake.files[name], want)
		}
	}
	for name, want := range map[string]string{"paperWidth": "210mm", "paperHeight": "297mm", "preferCssPageSize": "true", "failOnResourceLoadingFailed": "true"} {
		if fake.fields[name] != want {
			t.Errorf("field %s = %q, want %q", name, fake.fields[name], want)
		}
	}
}

func TestConvertHTMLErrors(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantConvert bool
	}{
		{name: "bad form", status: http.StatusBadRequest, body: "Invalid form data", wantConvert: true},
		{name: "resource failed", status: http.StatusConflict, body: "Chromium failed to load resources", wantConvert: true},
		{name: "unavailable", status: http.StatusServiceUnavailable, body: "Service Unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newFake(t, tt.status, tt.body)

			_, err := c.ConvertHTML(t.Context(), Document{HTML: []byte("<p>x</p>")}, "")
			if err == nil || !strings.Contains(err.Error(), tt.body) {
				t.Fatalf("err = %v, want containing %q", err, tt.body)
			}
			if errors.Is(err, ErrConvert) != tt.wantConvert {
				t.Errorf("errors.Is(ErrConvert) = %v, want %v", !tt.wantConvert, tt.wantConvert)
			}
		})
	}
}

func TestConvertHTMLRejectsInput(t *testing.T) {
	_, c := newFake(t, http.StatusOK, "%PDF")

	docs := map[string]Document{
		"empty html":     {},
		"reserved asset": {HTML: []byte("x"), Assets: map[string][]byte{"index.html": []byte("y")}},
		"asset path":     {HTML: []byte("x"), Assets: map[string][]byte{"../etc/passwd": []byte("y")}},
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			if _, err := c.ConvertHTML(t.Context(), doc, ""); !errors.Is(err, ErrConvert) {
				t.Errorf("err = %v, want ErrConvert", err)
			}
		})
	}
}

func TestConvertHTMLLimitsPDF(t *testing.T) {
	_, c := newFake(t, http.StatusOK, strings.Repeat("x", 100))
	c.maxPDF = 10

	if _, err := c.ConvertHTML(t.Context(), Document{HTML: []byte("x")}, ""); !errors.Is(err, ErrConvert) {
		t.Errorf("err = %v, want ErrConvert", err)
	}
}

// TestConvertHTMLIntegration — настоящий Gotenberg (make up).
func TestConvertHTMLIntegration(t *testing.T) {
	if os.Getenv("DOCGEN_INTEGRATION") != "1" {
		t.Skip("set DOCGEN_INTEGRATION=1 to run integration tests")
	}
	c := New(os.Getenv("GOTENBERG_URL"), 30*time.Second)

	pdf, err := c.ConvertHTML(t.Context(), Document{
		HTML:   []byte(`<!doctype html><html><head><meta charset="utf-8"><style>@page { size: A4 landscape; }</style></head><body><p>Хаттама / Протокол</p></body></html>`),
		Footer: []byte(`<html><body><p style="font-size:8px"><span class="pageNumber"></span> / <span class="totalPages"></span></p></body></html>`),
	}, "integration-test")
	if err != nil {
		t.Fatalf("ConvertHTML: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Errorf("result is not a PDF: %q", pdf[:min(len(pdf), 20)])
	}

	_, err = c.ConvertHTML(t.Context(), Document{HTML: []byte(`<img src="missing.png">`)}, "")
	if !errors.Is(err, ErrConvert) {
		t.Errorf("missing asset: err = %v, want ErrConvert", err)
	}
}
