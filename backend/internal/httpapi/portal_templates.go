package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"docgen/internal/db"
	"docgen/internal/templates"
)

// PortalTemplateService — то, что портальным эндпоинтам шаблонов нужно от сервиса шаблонов.
type PortalTemplateService interface {
	ListActive(ctx context.Context, project string) ([]db.ListActiveTemplatesByProjectRow, error)
	Resolve(ctx context.Context, project string, ref templates.Ref) (db.ResolveTemplateVersionRow, error)
}

// portalTemplateResponse — шаблон глазами портала: только то, что нужно для генерации.
// Внутренние id, статусы и черновики порталу не показываем.
type portalTemplateResponse struct {
	Code          string `json:"code"`
	Locale        string `json:"locale"`
	Name          string `json:"name"`
	Engine        string `json:"engine"`
	ActiveVersion int32  `json:"active_version"`
}

// portalSchemaResponse — схема данных конкретной версии шаблона.
type portalSchemaResponse struct {
	Code    string          `json:"code"`
	Locale  string          `json:"locale"`
	Version int32           `json:"version"`
	Schema  json.RawMessage `json:"schema"`
}

// listPortalTemplatesHandler отдаёт активные шаблоны проекта, которому принадлежит API-ключ.
// Проект берётся только из ключа: параметров для выбора проекта у запроса нет.
func listPortalTemplatesHandler(svc PortalTemplateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, ok := clientFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		list, err := svc.ListActive(r.Context(), client.Project)
		if err != nil {
			slog.ErrorContext(r.Context(), "list portal templates", "err", err)
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		resp := make([]portalTemplateResponse, 0, len(list))
		for _, t := range list {
			resp = append(resp, portalTemplateResponse(t))
		}

		writeJSON(w, r, http.StatusOK, resp)
	}
}

// portalSchemaHandler — GET /v1/templates/{code}/schema?locale=&version=.
// Без version — схема активной версии; locale обязателен, только если у кода несколько локалей.
func portalSchemaHandler(svc PortalTemplateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, ok := clientFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		ref := templates.Ref{Code: chi.URLParam(r, "code"), Locale: r.URL.Query().Get("locale")}
		if raw := r.URL.Query().Get("version"); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 32)
			if err != nil || n <= 0 {
				writeError(w, r, http.StatusBadRequest, "invalid_version", "version must be a positive integer")
				return
			}
			ref.Version = int32(n)
		}

		v, err := svc.Resolve(r.Context(), client.Project, ref)
		if err != nil {
			writeTemplateError(w, r, err)
			return
		}

		writeJSON(w, r, http.StatusOK, portalSchemaResponse{
			Code:    v.Code,
			Locale:  v.Locale,
			Version: v.Version,
			Schema:  v.DataSchema,
		})
	}
}
