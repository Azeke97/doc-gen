package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"docgen/internal/db"
	"docgen/internal/templates"
)

// TemplateService — то, что обработчикам шаблонов нужно от сервиса шаблонов.
type TemplateService interface {
	Create(ctx context.Context, actor string, p templates.CreateParams) (db.Template, error)
	Get(ctx context.Context, id uuid.UUID) (db.Template, error)
	List(ctx context.Context, project string) ([]db.Template, error)
	UploadVersion(ctx context.Context, actor string, templateID uuid.UUID, p templates.UploadParams) (db.TemplateVersion, bool, error)
	ListVersions(ctx context.Context, templateID uuid.UUID) ([]db.TemplateVersion, error)
	Publish(ctx context.Context, actor string, versionID uuid.UUID, opts templates.PublishOptions) (db.TemplateVersion, error)
	DiffVersion(ctx context.Context, versionID uuid.UUID) (templates.VersionDiff, error)
	Preview(ctx context.Context, versionID uuid.UUID, trace string) ([]byte, error)
	VersionFile(ctx context.Context, versionID uuid.UUID) (db.Template, db.TemplateVersion, []byte, error)
	Activate(ctx context.Context, actor string, templateID, versionID uuid.UUID, opts templates.ActivateOptions) (db.Template, error)
	Archive(ctx context.Context, actor string, templateID uuid.UUID) (db.Template, error)
}

type createTemplateRequest struct {
	Project       string `json:"project"`
	Code          string `json:"code"`
	Locale        string `json:"locale"`
	Name          string `json:"name"`
	Engine        string `json:"engine"`
	RetentionDays int32  `json:"retention_days"`
}

type templateResponse struct {
	ID              uuid.UUID  `json:"id"`
	Project         string     `json:"project"`
	Code            string     `json:"code"`
	Locale          string     `json:"locale"`
	Name            string     `json:"name"`
	Engine          string     `json:"engine"`
	Status          string     `json:"status"`
	RetentionDays   int32      `json:"retention_days"`
	ActiveVersionID *uuid.UUID `json:"active_version_id"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func toTemplateResponse(t db.Template) templateResponse {
	return templateResponse{
		ID:              t.ID,
		Project:         t.Project,
		Code:            t.Code,
		Locale:          t.Locale,
		Name:            t.Name,
		Engine:          t.Engine,
		Status:          t.Status,
		RetentionDays:   t.RetentionDays,
		ActiveVersionID: t.ActiveVersionID,
		CreatedAt:       t.CreatedAt,
		UpdatedAt:       t.UpdatedAt,
	}
}

func createTemplateHandler(svc TemplateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createTemplateRequest
		if !decodeJSON(w, r, &req) {
			return
		}

		actor, _ := actorFromContext(r.Context())
		tmpl, err := svc.Create(r.Context(), actor.Email, templates.CreateParams(req))
		if err != nil {
			writeTemplateError(w, r, err)
			return
		}

		writeJSON(w, r, http.StatusCreated, toTemplateResponse(tmpl))
	}
}

func listTemplatesHandler(svc TemplateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := svc.List(r.Context(), r.URL.Query().Get("project"))
		if err != nil {
			writeTemplateError(w, r, err)
			return
		}

		resp := make([]templateResponse, 0, len(list))
		for _, t := range list {
			resp = append(resp, toTemplateResponse(t))
		}

		writeJSON(w, r, http.StatusOK, resp)
	}
}

func getTemplateHandler(svc TemplateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}

		tmpl, err := svc.Get(r.Context(), id)
		if err != nil {
			writeTemplateError(w, r, err)
			return
		}

		writeJSON(w, r, http.StatusOK, toTemplateResponse(tmpl))
	}
}

// writeTemplateError переводит ошибки сервиса шаблонов в ответы API.
func writeTemplateError(w http.ResponseWriter, r *http.Request, err error) {
	var (
		vErr        *templates.ValidationError
		breakingErr *templates.BreakingChangesError
	)

	switch {
	case errors.As(err, &vErr) && len(vErr.Errors) > 0:
		writeErrorDetails(w, r, http.StatusBadRequest, "invalid_"+vErr.Field, vErr.Error(), vErr.Errors)
	case errors.As(err, &vErr):
		writeError(w, r, http.StatusBadRequest, "invalid_"+vErr.Field, vErr.Error())
	case errors.As(err, &breakingErr):
		writeErrorDetails(w, r, http.StatusConflict, "breaking_changes", breakingErr.Error(), toDiffResponse(breakingErr.Diff))
	case errors.Is(err, templates.ErrProjectNotFound):
		writeError(w, r, http.StatusUnprocessableEntity, "project_not_found", err.Error())
	case errors.Is(err, templates.ErrTemplateExists):
		writeError(w, r, http.StatusConflict, "template_exists", err.Error())
	case errors.Is(err, templates.ErrTemplateNotFound):
		writeError(w, r, http.StatusNotFound, "template_not_found", templates.ErrTemplateNotFound.Error())
	case errors.Is(err, templates.ErrTemplateArchived):
		writeError(w, r, http.StatusConflict, "template_archived", templates.ErrTemplateArchived.Error())
	case errors.Is(err, templates.ErrVersionNotFound):
		writeError(w, r, http.StatusNotFound, "version_not_found", templates.ErrVersionNotFound.Error())
	case errors.Is(err, templates.ErrVersionAlreadyPublished):
		writeError(w, r, http.StatusConflict, "version_already_published", templates.ErrVersionAlreadyPublished.Error())
	case errors.Is(err, templates.ErrVersionNotPublished):
		writeError(w, r, http.StatusConflict, "version_not_published", templates.ErrVersionNotPublished.Error())
	case errors.Is(err, templates.ErrNoTestData):
		writeError(w, r, http.StatusUnprocessableEntity, "no_test_data", templates.ErrNoTestData.Error())
	case errors.Is(err, templates.ErrPreviewNotSupported):
		writeError(w, r, http.StatusUnprocessableEntity, "preview_not_supported", templates.ErrPreviewNotSupported.Error())
	case errors.Is(err, templates.ErrRenderFailed):
		writeError(w, r, http.StatusUnprocessableEntity, "render_failed", err.Error())
	default:
		slog.ErrorContext(r.Context(), "template operation", "err", err)
		writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}
