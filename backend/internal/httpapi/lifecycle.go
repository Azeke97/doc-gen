package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"docgen/internal/dataschema"
	"docgen/internal/templates"
)

// diffResponse — изменения схемы версии относительно базовой.
type diffResponse struct {
	BaseVersion *int32              `json:"base_version"`
	Breaking    bool                `json:"breaking"`
	Changes     []dataschema.Change `json:"changes"`
}

func toDiffResponse(d templates.VersionDiff) diffResponse {
	return diffResponse{
		BaseVersion: d.BaseVersion,
		Breaking:    dataschema.HasBreaking(d.Changes),
		Changes:     d.Changes,
	}
}

type activateRequest struct {
	VersionID uuid.UUID `json:"version_id"`
}

func publishVersionHandler(svc TemplateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		versionID, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}

		// ?confirm_breaking=true — администратор видел ломающие изменения схемы (ответ 409)
		// и публикует осознанно.
		opts := templates.PublishOptions{ConfirmBreaking: r.URL.Query().Get("confirm_breaking") == "true"}

		actor, _ := actorFromContext(r.Context())
		version, err := svc.Publish(r.Context(), actor.Email, versionID, opts)
		if err != nil {
			writeTemplateError(w, r, err)
			return
		}

		writeJSON(w, r, http.StatusOK, toVersionResponse(version))
	}
}

func activateTemplateHandler(svc TemplateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		templateID, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}

		var req activateRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.VersionID == uuid.Nil {
			writeError(w, r, http.StatusBadRequest, "invalid_version_id", "version_id is required")
			return
		}

		actor, _ := actorFromContext(r.Context())
		tmpl, err := svc.Activate(r.Context(), actor.Email, templateID, req.VersionID)
		if err != nil {
			writeTemplateError(w, r, err)
			return
		}

		writeJSON(w, r, http.StatusOK, toTemplateResponse(tmpl))
	}
}

func archiveTemplateHandler(svc TemplateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		templateID, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}

		actor, _ := actorFromContext(r.Context())
		tmpl, err := svc.Archive(r.Context(), actor.Email, templateID)
		if err != nil {
			writeTemplateError(w, r, err)
			return
		}

		writeJSON(w, r, http.StatusOK, toTemplateResponse(tmpl))
	}
}

func diffVersionHandler(svc TemplateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		versionID, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}

		diff, err := svc.DiffVersion(r.Context(), versionID)
		if err != nil {
			writeTemplateError(w, r, err)
			return
		}

		writeJSON(w, r, http.StatusOK, toDiffResponse(diff))
	}
}
