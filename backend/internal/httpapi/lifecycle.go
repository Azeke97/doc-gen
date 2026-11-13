package httpapi

import (
	"net/http"

	"github.com/google/uuid"
)

type activateRequest struct {
	VersionID uuid.UUID `json:"version_id"`
}

func publishVersionHandler(svc TemplateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		versionID, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}

		actor, _ := actorFromContext(r.Context())
		version, err := svc.Publish(r.Context(), actor.Email, versionID)
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
