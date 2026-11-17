package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"docgen/internal/db"
	"docgen/internal/pgerr"
)

// projectCodePattern повторяет CHECK из миграции projects: проверяем заранее, чтобы ответить 400, а не 500.
var projectCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,49}$`)

// maxRetentionDays — предел срока хранения проекта: 100 лет.
const maxRetentionDays = 36500

// ProjectStore — то, что обработчикам проектов нужно от хранилища.
type ProjectStore interface {
	CreateProject(ctx context.Context, arg db.CreateProjectParams) (db.Project, error)
	UpdateProject(ctx context.Context, arg db.UpdateProjectParams) (db.Project, error)
	ListProjects(ctx context.Context) ([]db.Project, error)
}

type createProjectRequest struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	RetentionDays *int32 `json:"retention_days"` // без него — 365 дней
}

type updateProjectRequest struct {
	Name          *string `json:"name"`
	RetentionDays *int32  `json:"retention_days"`
}

type projectResponse struct {
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	RetentionDays int32     `json:"retention_days"`
	CreatedAt     time.Time `json:"created_at"`
}

func toProjectResponse(p db.Project) projectResponse {
	return projectResponse{Code: p.Code, Name: p.Name, RetentionDays: p.RetentionDays, CreatedAt: p.CreatedAt}
}

// validRetention: срок хранения — от 1 дня до maxRetentionDays; nil — не задан.
func validRetention(days *int32) bool {
	return days == nil || (*days >= 1 && *days <= maxRetentionDays)
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
		if !validRetention(req.RetentionDays) {
			writeError(w, r, http.StatusBadRequest, "invalid_retention_days", fmt.Sprintf("retention_days must be from 1 to %d", maxRetentionDays))
			return
		}

		project, err := store.CreateProject(r.Context(), db.CreateProjectParams{Code: req.Code, Name: req.Name, RetentionDays: req.RetentionDays})
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

// updateProjectHandler — PATCH /admin/v1/projects/{code}: имя и срок хранения. Новый срок
// действует для документов, выпущенных после изменения; у выпущенных срок не меняется.
func updateProjectHandler(store ProjectStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req updateProjectRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				writeError(w, r, http.StatusBadRequest, "invalid_name", "name must not be empty")
				return
			}
			req.Name = &name
		}
		if !validRetention(req.RetentionDays) {
			writeError(w, r, http.StatusBadRequest, "invalid_retention_days", fmt.Sprintf("retention_days must be from 1 to %d", maxRetentionDays))
			return
		}

		project, err := store.UpdateProject(r.Context(), db.UpdateProjectParams{
			Code: chi.URLParam(r, "code"), Name: req.Name, RetentionDays: req.RetentionDays,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "project_not_found", "project not found")
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "update project", "err", err)
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		actor, _ := actorFromContext(r.Context())
		slog.InfoContext(r.Context(), "project updated", "code", project.Code, "retention_days", project.RetentionDays, "actor", actor.Email)
		writeJSON(w, r, http.StatusOK, toProjectResponse(project))
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
