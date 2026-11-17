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
	gotUpload  templates.UploadParams
	created    bool
	gotIDs     []uuid.UUID
	gotOpts    templates.PublishOptions

	gotActivateOpts templates.ActivateOptions
	diff            templates.VersionDiff

	pdf      []byte
	gotTrace string

	tmpl    db.Template
	version db.TemplateVersion
	file    []byte
}

func (f *fakeTemplateService) Create(_ context.Context, actor string, p templates.CreateParams) (db.Template, error) {
	f.gotActor, f.gotParams = actor, p
	if f.err != nil {
		return db.Template{}, f.err
	}
	return db.Template{ID: uuid.New(), Project: p.Project, Code: p.Code, Locale: p.Locale, Status: "draft"}, nil
}

func (f *fakeTemplateService) Get(_ context.Context, id uuid.UUID) (db.Template, error) {
	f.gotIDs = []uuid.UUID{id}
	return f.tmpl, f.err
}

func (f *fakeTemplateService) VersionFile(_ context.Context, versionID uuid.UUID) (db.Template, db.TemplateVersion, []byte, error) {
	f.gotIDs = []uuid.UUID{versionID}
	return f.tmpl, f.version, f.file, f.err
}

func (f *fakeTemplateService) List(_ context.Context, project string) ([]db.Template, error) {
	f.gotProject = project
	return nil, f.err
}

func (f *fakeTemplateService) UploadVersion(_ context.Context, actor string, templateID uuid.UUID, p templates.UploadParams) (db.TemplateVersion, bool, error) {
	f.gotActor, f.gotUpload = actor, p
	if f.err != nil {
		return db.TemplateVersion{}, false, f.err
	}
	return db.TemplateVersion{ID: uuid.New(), TemplateID: templateID, Version: 1, Status: "draft"}, f.created, nil
}

func (f *fakeTemplateService) ListVersions(_ context.Context, _ uuid.UUID) ([]db.TemplateVersion, error) {
	return nil, f.err
}

func (f *fakeTemplateService) Publish(_ context.Context, actor string, versionID uuid.UUID, opts templates.PublishOptions) (db.TemplateVersion, error) {
	f.gotActor, f.gotIDs, f.gotOpts = actor, []uuid.UUID{versionID}, opts
	return db.TemplateVersion{ID: versionID, Status: "published", PublishedBy: &actor}, f.err
}

func (f *fakeTemplateService) DiffVersion(_ context.Context, versionID uuid.UUID) (templates.VersionDiff, error) {
	f.gotIDs = []uuid.UUID{versionID}
	return f.diff, f.err
}

func (f *fakeTemplateService) Preview(_ context.Context, versionID uuid.UUID, trace string) ([]byte, error) {
	f.gotIDs, f.gotTrace = []uuid.UUID{versionID}, trace
	return f.pdf, f.err
}

func (f *fakeTemplateService) Activate(_ context.Context, actor string, templateID, versionID uuid.UUID, opts templates.ActivateOptions) (db.Template, error) {
	f.gotActor, f.gotIDs, f.gotActivateOpts = actor, []uuid.UUID{templateID, versionID}, opts
	return db.Template{ID: templateID, Status: "active", ActiveVersionID: &versionID}, f.err
}

func (f *fakeTemplateService) Archive(_ context.Context, actor string, templateID uuid.UUID) (db.Template, error) {
	f.gotActor, f.gotIDs = actor, []uuid.UUID{templateID}
	return db.Template{ID: templateID, Status: "archived"}, f.err
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
