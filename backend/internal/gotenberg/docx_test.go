package gotenberg

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// minimalDocx — наименьший DOCX, который открывает LibreOffice.
func minimalDocx(t *testing.T, text string) []byte {
	t.Helper()
	files := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestConvertDOCX(t *testing.T) {
	fake, c := newFake(t, http.StatusOK, "%PDF-1.7 fake")

	pdf, err := c.ConvertDOCX(t.Context(), []byte("PK docx"), "trace-1")
	if err != nil {
		t.Fatalf("ConvertDOCX: %v", err)
	}
	if string(pdf) != "%PDF-1.7 fake" {
		t.Errorf("pdf = %q", pdf)
	}
	if fake.path != "/forms/libreoffice/convert" || fake.trace != "trace-1" {
		t.Errorf("path = %q, trace = %q", fake.path, fake.trace)
	}
	if fake.files["document.docx"] != "PK docx" || len(fake.files) != 1 {
		t.Errorf("files = %v", fake.files)
	}

	if _, err := c.ConvertDOCX(t.Context(), nil, ""); !errors.Is(err, ErrConvert) {
		t.Errorf("empty document: err = %v, want ErrConvert", err)
	}
}

func TestConvertDOCXErrors(t *testing.T) {
	tests := []struct {
		name            string
		status          int
		wantConvert     bool
		wantUnavailable bool
	}{
		{name: "bad document", status: http.StatusBadRequest, wantConvert: true},
		{name: "busy", status: http.StatusServiceUnavailable, wantUnavailable: true},
		{name: "rate limited", status: http.StatusTooManyRequests, wantUnavailable: true},
		{name: "internal", status: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newFake(t, tt.status, "message")
			_, err := c.ConvertDOCX(t.Context(), []byte("x"), "")
			if err == nil {
				t.Fatal("want error")
			}
			if errors.Is(err, ErrConvert) != tt.wantConvert || errors.Is(err, ErrUnavailable) != tt.wantUnavailable {
				t.Errorf("err = %v: ErrConvert %v (want %v), ErrUnavailable %v (want %v)",
					err, errors.Is(err, ErrConvert), tt.wantConvert, errors.Is(err, ErrUnavailable), tt.wantUnavailable)
			}
		})
	}

	t.Run("no connection", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close()
		_, err := New(srv.URL, time.Second).ConvertDOCX(t.Context(), []byte("x"), "")
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("err = %v, want ErrUnavailable", err)
		}
	})
}

func TestConcurrencyLimit(t *testing.T) {
	release := make(chan struct{})
	var inFlight, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		<-release
		inFlight.Add(-1)
		_, _ = w.Write([]byte("%PDF"))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, 5*time.Second).WithConcurrency(2)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if _, err := c.ConvertDOCX(context.Background(), []byte("x"), ""); err != nil {
				t.Errorf("ConvertDOCX: %v", err)
			}
		})
	}
	time.Sleep(200 * time.Millisecond) // все пять запросов успели дойти до семафора
	if p := peak.Load(); p != 2 {
		t.Errorf("peak concurrent conversions = %d, want 2", p)
	}
	close(release)
	wg.Wait()
}

func TestWaitForSlotRespectsContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, _ = w.Write([]byte("%PDF"))
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	c := New(srv.URL, 5*time.Second).WithConcurrency(1)
	go func() { _, _ = c.ConvertDOCX(context.Background(), []byte("x"), "") }()
	time.Sleep(50 * time.Millisecond) // первый запрос занял единственный слот

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := c.ConvertDOCX(ctx, []byte("x"), ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

// TestConvertDOCXIntegration — настоящий Gotenberg (make up).
func TestConvertDOCXIntegration(t *testing.T) {
	if os.Getenv("DOCGEN_INTEGRATION") != "1" {
		t.Skip("set DOCGEN_INTEGRATION=1 to run integration tests")
	}
	c := New(os.Getenv("GOTENBERG_URL"), 30*time.Second)

	pdf, err := c.ConvertDOCX(t.Context(), minimalDocx(t, "Хаттама / Протокол: ә ғ қ ң ө ұ ү һ і"), "integration-test")
	if err != nil {
		t.Fatalf("ConvertDOCX: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Errorf("result is not a PDF: %q", pdf[:min(len(pdf), 20)])
	}

}
