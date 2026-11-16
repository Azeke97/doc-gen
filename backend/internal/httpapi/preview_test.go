package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"docgen/internal/templates"
)

func TestPreviewVersion(t *testing.T) {
	s := newAdminTestServer(t)
	s.templates.pdf = []byte("%PDF-1.7 preview")
	token := s.login()
	versionID := uuid.New()

	rec := s.do(http.MethodGet, "/admin/v1/versions/"+versionID.String()+"/preview", "", token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "%PDF-1.7 preview" {
		t.Errorf("body = %q", got)
	}
	wantHeaders := map[string]string{
		"Content-Type":           "application/pdf",
		"Content-Disposition":    fmt.Sprintf(`inline; filename="preview-%s.pdf"`, versionID),
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
	}
	for name, want := range wantHeaders {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if s.templates.gotIDs[0] != versionID || s.templates.gotTrace == "" {
		t.Errorf("Preview(%v, trace %q), want %s and a request id", s.templates.gotIDs, s.templates.gotTrace, versionID)
	}
}

func TestPreviewVersionErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "unknown version", err: templates.ErrVersionNotFound, wantStatus: http.StatusNotFound, wantCode: "version_not_found"},
		{name: "no test data", err: templates.ErrNoTestData, wantStatus: http.StatusUnprocessableEntity, wantCode: "no_test_data"},
		{name: "docx", err: templates.ErrPreviewNotSupported, wantStatus: http.StatusUnprocessableEntity, wantCode: "preview_not_supported"},
		{name: "render failed", err: fmt.Errorf("%w: range can't iterate", templates.ErrRenderFailed), wantStatus: http.StatusUnprocessableEntity, wantCode: "render_failed"},
		{name: "gotenberg down", err: fmt.Errorf("convert to pdf: connection refused"), wantStatus: http.StatusInternalServerError, wantCode: "internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newAdminTestServer(t)
			s.templates.err = tt.err
			token := s.login()

			rec := s.do(http.MethodGet, "/admin/v1/versions/"+uuid.NewString()+"/preview", "", token)

			assertErrorCode(t, rec, tt.wantStatus, tt.wantCode)
		})
	}
}
