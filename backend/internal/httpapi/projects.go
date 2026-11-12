package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"docgen/internal/db"
	"docgen/internal/pgerr"
)

// projectCodePattern повторяет CHECK из миграции projects: проверяем заранее, чтобы ответить 400, а не 500.
var projectCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,49}$`)

// ProjectStore — то, что обработчикам проектов нужно от хранилища.
type ProjectStore interface {
	CreateProject(ctx context.Context, arg db.CreateProjectParams) (db.Project, error)
	ListProjects(ctx context.Context) ([]db.Project, error)
}

type createProjectRequest struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type projectResponse struct {
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func toProjectResponse(p db.Project) projectResponse {
	return projectResponse{Code: p.Code, Name: p.Name, CreatedAt: p.CreatedAt}
}

func createProjectHandler(store ProjectStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createProjectRequest
		if !decodeJSON(w, r, &req) {
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		if !projectCodePattern.MatchString(req.Code) {
			writeError(w, r, http.StatusBadRequest, "invalid_code",
				"code must start with a latin letter and contain only a-z, 0-9 and _ (2-50 chars)")
			return
		}
		if req.Name == "" {
			writeError(w, r, http.StatusBadRequest, "invalid_name", "name is required")
			return
		}

		project, err := store.CreateProject(r.Context(), db.CreateProjectParams{Code: req.Code, Name: req.Name})
		if pgerr.IsUniqueViolation(err) {
			writeError(w, r, http.StatusConflict, "project_exists", "project with this code already exists")
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "create project", "err", err)
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		actor, _ := actorFromContext(r.Context())
		slog.InfoContext(r.Context(), "project created", "code", project.Code, "actor", actor.Email)

		writeJSON(w, r, http.StatusCreated, toProjectResponse(project))
	}
}

func listProjectsHandler(store ProjectStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projects, err := store.ListProjects(r.Context())
		if err != nil {
			slog.ErrorContext(r.Context(), "list projects", "err", err)
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		resp := make([]projectResponse, 0, len(projects))
		for _, p := range projects {
			resp = append(resp, toProjectResponse(p))
		}

		writeJSON(w, r, http.StatusOK, resp)
	}
}
