package httpapi

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"docgen/internal/db"
	"docgen/internal/templates"
)

// maxUploadBody — предел всего multipart-запроса: файл плюс схема и тестовые данные.
const maxUploadBody = templates.MaxFileSize + 2<<20

type versionResponse struct {
	ID          uuid.UUID       `json:"id"`
	TemplateID  uuid.UUID       `json:"template_id"`
	Version     int32           `json:"version"`
	Status      string          `json:"status"`
	FileSHA256  string          `json:"file_sha256"`
	DataSchema  json.RawMessage `json:"data_schema"`
	TestData    json.RawMessage `json:"test_data"`
	CreatedBy   string          `json:"created_by"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	PublishedBy *string         `json:"published_by"`
	PublishedAt *time.Time      `json:"published_at"`
}

func toVersionResponse(v db.TemplateVersion) versionResponse {
	return versionResponse{
		ID:          v.ID,
		TemplateID:  v.TemplateID,
		Version:     v.Version,
		Status:      v.Status,
		FileSHA256:  hex.EncodeToString(v.FileSha256),
		DataSchema:  v.DataSchema,
		TestData:    v.TestData,
		CreatedBy:   v.CreatedBy,
		CreatedAt:   v.CreatedAt,
		UpdatedAt:   v.UpdatedAt,
		PublishedBy: v.PublishedBy,
		PublishedAt: v.PublishedAt,
	}
}

// uploadVersionHandler принимает multipart/form-data: file — файл шаблона,
// schema — JSON Schema данных, test_data — тестовые данные (необязательно).
func uploadVersionHandler(svc TemplateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		templateID, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBody)
		if err := r.ParseMultipartForm(maxUploadBody); err != nil { //nolint:gosec // размер ограничен MaxBytesReader строкой выше
			writeError(w, r, http.StatusBadRequest, "invalid_form",
				"expected multipart/form-data with file and schema: "+err.Error())
			return
		}

		file, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_file", "field file is required")
			return
		}
		defer func() { _ = file.Close() }()

		content, err := io.ReadAll(file)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_file", "cannot read file: "+err.Error())
			return
		}

		params := templates.UploadParams{
			File:   content,
			Schema: json.RawMessage(r.FormValue("schema")),
		}
		if td := r.FormValue("test_data"); td != "" {
			params.TestData = json.RawMessage(td)
		}

		actor, _ := actorFromContext(r.Context())
		version, created, err := svc.UploadVersion(r.Context(), actor.Email, templateID, params)
		if err != nil {
			writeTemplateError(w, r, err)
			return
		}

		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, r, status, toVersionResponse(version))
	}
}

func listVersionsHandler(svc TemplateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		templateID, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}

		list, err := svc.ListVersions(r.Context(), templateID)
		if err != nil {
			writeTemplateError(w, r, err)
			return
		}

		resp := make([]versionResponse, 0, len(list))
		for _, v := range list {
			resp = append(resp, toVersionResponse(v))
		}

		writeJSON(w, r, http.StatusOK, resp)
	}
}

// parseUUIDParam читает UUID из параметра маршрута. При ошибке сам отвечает 400.
func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_"+name, name+" must be a UUID")
		return uuid.UUID{}, false
	}

	return id, true
}
