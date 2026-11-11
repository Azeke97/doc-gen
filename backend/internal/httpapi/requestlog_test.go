package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	router := NewRouter(Deps{Logger: logger, APIClients: &fakeClientStore{}})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/whoami", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)

	var entry struct {
		Msg       string `json:"msg"`
		Method    string `json:"method"`
		Path      string `json:"path"`
		Status    int    `json:"status"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}

	if entry.Msg != "http request" || entry.Method != http.MethodGet || entry.Path != "/v1/whoami" {
		t.Errorf("unexpected log entry: %+v", entry)
	}
	if entry.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", entry.Status, http.StatusUnauthorized)
	}
	if entry.RequestID == "" {
		t.Error("request_id is empty")
	}
}

func TestRequestLoggerHealthzIsDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	router := NewRouter(Deps{Logger: logger, APIClients: &fakeClientStore{}})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)

	if buf.Len() != 0 {
		t.Errorf("healthz logged at info level: %s", buf.String())
	}
}
