package gotenberg

import (
	"io"
	"net/http"
	"net/http/httptest"
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
