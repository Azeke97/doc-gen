package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"docgen/internal/db"
	"docgen/internal/templates"
)

// fakeTemplateService запоминает, с чем его вызвали, и возвращает заданный результат.
type fakeTemplateService struct {
	err        error
	gotActor   string
	gotParams  templates.CreateParams
	gotProject string
}

func (f *fakeTemplateService) Create(_ context.Context, actor string, p templates.CreateParams) (db.Template, error) {
	f.gotActor, f.gotParams = actor, p
	if f.err != nil {
		return db.Template{}, f.err
	}
	return db.Template{ID: uuid.New(), Project: p.Project, Code: p.Code, Locale: p.Locale, Status: "draft"}, nil
}

func (f *fakeTemplateService) List(_ context.Context, project string) ([]db.Template, error) {
	f.gotProject = project
	return nil, f.err
}

func TestCreateTemplate(t *testing.T) {
	body := `{"project":"procurement","code":"opening_protocol","locale":"bilingual","name":"Протокол вскрытия","engine":"html","retention_days":1825}`

	tests := []struct {
		name       string
		svcErr     error
		wantStatus int
		wantCode   string
	}{
		{name: "created", wantStatus: http.StatusCreated},
		{name: "invalid", svcErr: &templates.ValidationError{Field: "locale", Message: "bad"}, wantStatus: http.StatusBadRequest, wantCode: "invalid_locale"},
		{name: "no project", svcErr: templates.ErrProjectNotFound, wantStatus: http.StatusUnprocessableEntity, wantCode: "project_not_found"},
		{name: "duplicate", svcErr: templates.ErrTemplateExists, wantStatus: http.StatusConflict, wantCode: "template_exists"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newAdminTestServer(t)
			s.templates.err = tt.svcErr
			token := s.login()

			rec := s.do(http.MethodPost, "/admin/v1/templates", body, token)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if s.templates.gotActor != "admin@example.com" {
				t.Errorf("actor = %q, want admin@example.com", s.templates.gotActor)
			}
			if s.templates.gotParams.RetentionDays != 1825 || s.templates.gotParams.Locale != "bilingual" {
				t.Errorf("params not passed through: %+v", s.templates.gotParams)
			}
			if tt.wantCode == "" {
				return
			}

			var resp errorResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if resp.Error.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", resp.Error.Code, tt.wantCode)
			}
		})
	}
}

func TestListTemplatesFilter(t *testing.T) {
	s := newAdminTestServer(t)
	token := s.login()

	rec := s.do(http.MethodGet, "/admin/v1/templates?project=procurement", "", token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if s.templates.gotProject != "procurement" {
		t.Errorf("project filter = %q, want procurement", s.templates.gotProject)
	}
	if got := rec.Body.String(); got != "[]\n" {
		t.Errorf("empty list body = %q, want []", got)
	}
}
