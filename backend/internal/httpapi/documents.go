package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"docgen/internal/db"
	"docgen/internal/documents"
)

// maxDocumentRequest — предел тела запроса на генерацию: данные протокола бывают большими.
const maxDocumentRequest = 5 << 20 // 5 МиБ

const idempotencyKeyHeader = "Idempotency-Key"

// DocumentService — то, что эндпоинтам документов нужно от сервиса документов.
type DocumentService interface {
	Generate(ctx context.Context, client db.APIClient, req documents.GenerateRequest, trace string) (db.Document, bool, error)
	Get(ctx context.Context, project string, id uuid.UUID) (db.GetProjectDocumentRow, error)
	File(ctx context.Context, project string, id uuid.UUID) (db.GetProjectDocumentRow, []byte, error)
}

type generateDocumentRequest struct {
	Template    string          `json:"template"`
	Locale      string          `json:"locale"`
	Version     int32           `json:"version"`
	Format      string          `json:"format"`
	Data        json.RawMessage `json:"data"`
	ExternalID  *string         `json:"external_id"`
	RequestedBy *string         `json:"requested_by"`
}

type documentResponse struct {
	ID          uuid.UUID `json:"id"`
	PublicID    string    `json:"public_id"`
	Template    string    `json:"template"`
	Locale      string    `json:"locale"`
	Version     int32     `json:"version"`
	Format      string    `json:"format"`
	ExternalID  *string   `json:"external_id"`
	RequestedBy *string   `json:"requested_by"`
	FileSHA256  string    `json:"file_sha256"`
	FileSize    int64     `json:"file_size"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func toDocumentResponse(d db.GetProjectDocumentRow) documentResponse {
	return documentResponse{
		ID:          d.Document.ID,
		PublicID:    d.Document.PublicID,
		Template:    d.TemplateCode,
		Locale:      d.Locale,
		Version:     d.Version,
		Format:      d.Document.Format,
		ExternalID:  d.Document.ExternalID,
		RequestedBy: d.Document.RequestedBy,
		FileSHA256:  hex.EncodeToString(d.Document.FileSha256),
		FileSize:    d.Document.FileSize,
		CreatedAt:   d.Document.CreatedAt,
		ExpiresAt:   d.Document.ExpiresAt,
	}
}

// generateDocumentHandler — POST /v1/documents. Новый документ — 201, повтор с тем же
// Idempotency-Key — 200 и тот же документ.
func generateDocumentHandler(svc DocumentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, ok := clientFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		var req generateDocumentRequest
		if !decodeJSONLimit(w, r, &req, maxDocumentRequest) {
			return
		}

		doc, created, err := svc.Generate(r.Context(), client, documents.GenerateRequest{
			Template:       req.Template,
			Locale:         req.Locale,
			Version:        req.Version,
			Format:         req.Format,
			Data:           req.Data,
			ExternalID:     req.ExternalID,
			RequestedBy:    req.RequestedBy,
			IdempotencyKey: r.Header.Get(idempotencyKeyHeader),
		}, middleware.GetReqID(r.Context()))
		if err != nil {
			writeDocumentError(w, r, err)
			return
		}

		view, err := svc.Get(r.Context(), client.Project, doc.ID)
		if err != nil {
			writeDocumentError(w, r, err)
			return
		}

		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		w.Header().Set("Location", "/v1/documents/"+doc.ID.String())
		writeJSON(w, r, status, toDocumentResponse(view))
	}
}

func getDocumentHandler(svc DocumentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, ok := clientFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		id, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}

		doc, err := svc.Get(r.Context(), client.Project, id)
		if err != nil {
			writeDocumentError(w, r, err)
			return
		}

		writeJSON(w, r, http.StatusOK, toDocumentResponse(doc))
	}
}

// documentFileHandler отдаёт файл документа на скачивание.
func documentFileHandler(svc DocumentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, ok := clientFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		id, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}

		doc, content, err := svc.File(r.Context(), client.Project, id)
		if err != nil {
			writeDocumentError(w, r, err)
			return
		}

		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.pdf"`, doc.TemplateCode, doc.Document.PublicID))
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(content); err != nil { //nolint:gosec // ответ — PDF с nosniff, браузер не выполнит его как HTML
			slog.ErrorContext(r.Context(), "write document file", "err", err)
		}
	}
}

// writeDocumentError переводит ошибки сервиса документов в ответы API.
func writeDocumentError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, documents.ErrIdempotencyConflict):
		writeError(w, r, http.StatusConflict, "idempotency_conflict", documents.ErrIdempotencyConflict.Error())
	case errors.Is(err, documents.ErrDocumentNotFound):
		writeError(w, r, http.StatusNotFound, "document_not_found", documents.ErrDocumentNotFound.Error())
	case errors.Is(err, documents.ErrDocumentExpired):
		writeError(w, r, http.StatusGone, "document_expired", documents.ErrDocumentExpired.Error())
	default:
		writeTemplateError(w, r, err)
	}
}
