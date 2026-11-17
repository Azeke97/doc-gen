package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"docgen/internal/db"
)

// fakeProjectStore — подделка хранилища проектов в памяти.
type fakeProjectStore struct {
	projects map[string]db.Project
}

func (f *fakeProjectStore) CreateProject(_ context.Context, arg db.CreateProjectParams) (db.Project, error) {
	if _, ok := f.projects[arg.Code]; ok {
		return db.Project{}, &pgconn.PgError{Code: "23505"}
	}

	p := db.Project{Code: arg.Code, Name: arg.Name, RetentionDays: 365}
	if arg.RetentionDays != nil {
		p.RetentionDays = *arg.RetentionDays
	}
	f.projects[arg.Code] = p

	return p, nil
}

func (f *fakeProjectStore) UpdateProject(_ context.Context, arg db.UpdateProjectParams) (db.Project, error) {
	p, ok := f.projects[arg.Code]
	if !ok {
		return db.Project{}, pgx.ErrNoRows
	}
	if arg.Name != nil {
		p.Name = *arg.Name
	}
	if arg.RetentionDays != nil {
		p.RetentionDays = *arg.RetentionDays
	}
	f.projects[arg.Code] = p
	return p, nil
}

func (f *fakeProjectStore) ListProjects(_ context.Context) ([]db.Project, error) {
	list := make([]db.Project, 0, len(f.projects))
	for _, p := range f.projects {
		list = append(list, p)
	}

	return list, nil
}

func TestProjectsRequireSession(t *testing.T) {
	s := newAdminTestServer(t)

	if rec := s.do(http.MethodGet, "/admin/v1/projects", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if rec := s.do(http.MethodGet, "/admin/v1/projects", "", "dgs_forged"); rec.Code != http.StatusUnauthorized {
		t.Errorf("forged token: status = %d, want 401", rec.Code)
	}
}

func TestCreateProject(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{name: "created", body: `{"code":"procurement","name":"Закупки"}`, wantStatus: http.StatusCreated},
		{name: "duplicate", body: `{"code":"auctions","name":"Аукционы"}`, wantStatus: http.StatusConflict, wantCode: "project_exists"},
		{name: "bad code", body: `{"code":"Закупки","name":"x"}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_code"},
		{name: "empty name", body: `{"code":"ok_code","name":"  "}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_name"},
		{name: "unknown field", body: `{"code":"x1","name":"x","extra":1}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
		{name: "not json", body: `code=x`, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newAdminTestServer(t)
			s.projects.projects["auctions"] = db.Project{Code: "auctions"}
			token := s.login()

			rec := s.do(http.MethodPost, "/admin/v1/projects", tt.body, token)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantCode == "" {
				return
			}

			var body errorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Error.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", body.Error.Code, tt.wantCode)
			}
		})
	}
}

func TestProjectRetention(t *testing.T) {
	s := newAdminTestServer(t)
	token := s.login()

	rec := s.do(http.MethodPost, "/admin/v1/projects", `{"code":"procurement","name":"Закупки","retention_days":1825}`, token)
	var p projectResponse
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil || rec.Code != http.StatusCreated || p.RetentionDays != 1825 {
		t.Fatalf("create: status %d, %+v, %v", rec.Code, p, err)
	}

	rec = s.do(http.MethodPatch, "/admin/v1/projects/procurement", `{"retention_days":30}`, token)
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil || rec.Code != http.StatusOK || p.RetentionDays != 30 || p.Name != "Закупки" {
		t.Fatalf("update: status %d, %+v, %v", rec.Code, p, err)
	}

	assertErrorCode(t, s.do(http.MethodPatch, "/admin/v1/projects/procurement", `{"retention_days":0}`, token), http.StatusBadRequest, "invalid_retention_days")
	assertErrorCode(t, s.do(http.MethodPatch, "/admin/v1/projects/procurement", `{"name":"  "}`, token), http.StatusBadRequest, "invalid_name")
	assertErrorCode(t, s.do(http.MethodPatch, "/admin/v1/projects/nope", `{"name":"x"}`, token), http.StatusNotFound, "project_not_found")
	assertErrorCode(t, s.do(http.MethodPost, "/admin/v1/projects", `{"code":"other","name":"x","retention_days":40000}`, token), http.StatusBadRequest, "invalid_retention_days")
}
