package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"docgen/internal/templates"
)

func TestLifecycleEndpoints(t *testing.T) {
	tmplID, versionID := uuid.NewString(), uuid.NewString()

	tests := []struct {
		name       string
		path       string
		body       string
		svcErr     error
		wantStatus int
		wantCode   string
	}{
		{name: "publish", path: "/admin/v1/versions/" + versionID + "/publish", wantStatus: http.StatusOK},
		{name: "publish twice", path: "/admin/v1/versions/" + versionID + "/publish", svcErr: templates.ErrVersionAlreadyPublished, wantStatus: http.StatusConflict, wantCode: "version_already_published"},
		{name: "publish unknown", path: "/admin/v1/versions/" + versionID + "/publish", svcErr: templates.ErrVersionNotFound, wantStatus: http.StatusNotFound, wantCode: "version_not_found"},
		{name: "publish bad id", path: "/admin/v1/versions/nope/publish", wantStatus: http.StatusBadRequest, wantCode: "invalid_id"},

		{name: "activate", path: "/admin/v1/templates/" + tmplID + "/activate", body: `{"version_id":"` + versionID + `"}`, wantStatus: http.StatusOK},
		{name: "activate draft", path: "/admin/v1/templates/" + tmplID + "/activate", body: `{"version_id":"` + versionID + `"}`, svcErr: templates.ErrVersionNotPublished, wantStatus: http.StatusConflict, wantCode: "version_not_published"},
		{name: "activate no version", path: "/admin/v1/templates/" + tmplID + "/activate", body: `{}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_version_id"},
		{name: "activate bad json", path: "/admin/v1/templates/" + tmplID + "/activate", body: `{"version_id":"nope"}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},

		{name: "archive", path: "/admin/v1/templates/" + tmplID + "/archive", wantStatus: http.StatusOK},
		{name: "archive twice", path: "/admin/v1/templates/" + tmplID + "/archive", svcErr: templates.ErrTemplateArchived, wantStatus: http.StatusConflict, wantCode: "template_archived"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newAdminTestServer(t)
			s.templates.err = tt.svcErr
			token := s.login()

			rec := s.do(http.MethodPost, tt.path, tt.body, token)

			if tt.wantCode != "" {
				assertErrorCode(t, rec, tt.wantStatus, tt.wantCode)
				return
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if s.templates.gotActor != "admin@example.com" {
				t.Errorf("actor = %q, want admin@example.com", s.templates.gotActor)
			}
		})
	}
}
