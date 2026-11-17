package httpapi

import (
	"context"
	"encoding/hex"
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
	"docgen/internal/docx"
	"docgen/internal/gotenberg"
)

const idempotencyKeyHeader = "Idempotency-Key"

// Пределы частей multipart-запроса на генерацию: шаблон, данные и короткие поля.
var generateFormLimits = map[string]int64{
	"template":       docx.MaxFileSize,
	"data":           documents.MaxData,
	"format":         16,
	"template_name":  documents.MaxTemplateName * 4, // в байтах: имя может быть кириллицей
	"external_id":    documents.MaxExternalID * 4,
	"requested_by":   documents.MaxRequestedBy * 4,
	"retention_days": 16,
}

// DocumentService — то, что эндпоинтам документов нужно от сервиса документов.
type DocumentService interface {
	GenerateDOCX(ctx context.Context, client db.APIClient, req documents.DocxRequest, trace string) (documents.Generated, error)
	CheckTemplate(file []byte) ([]docx.Variable, error)
	Get(ctx context.Context, project string, id uuid.UUID) (db.Document, error)
	File(ctx context.Context, project string, id uuid.UUID) (db.Document, []byte, error)
}

// JournalStore — запись в журнал запросов на генерацию.
type JournalStore interface {
	CreateGenerationLog(ctx context.Context, arg db.CreateGenerationLogParams) error
}

type documentResponse struct {
	ID             uuid.UUID `json:"id"`
	TemplateName   string    `json:"template_name"`
	TemplateSHA256 string    `json:"template_sha256"`
	Format         string    `json:"format"`
	ExternalID     *string   `json:"external_id"`
	RequestedBy    *string   `json:"requested_by"`
	FileSHA256     string    `json:"file_sha256"`
	FileSize       int64     `json:"file_size"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

func toDocumentResponse(d db.Document) documentResponse {
	return documentResponse{
		ID:             d.ID,
		TemplateName:   d.TemplateName,
		TemplateSHA256: hex.EncodeToString(d.TemplateSha256),
		Format:         d.Format,
		ExternalID:     d.ExternalID,
		RequestedBy:    d.RequestedBy,
		FileSHA256:     hex.EncodeToString(d.FileSha256),
		FileSize:       d.FileSize,
		CreatedAt:      d.CreatedAt,
		ExpiresAt:      d.ExpiresAt,
	}
}

// apiError — ошибка, как её увидит клиент и как она попадёт в журнал.
type apiError struct {
	status  int
	code    string
	message string
	details any
}

func (e *apiError) write(w http.ResponseWriter, r *http.Request) {
	writeErrorDetails(w, r, e.status, e.code, e.message, e.details)
}

// generateDocumentHandler — POST /v1/documents. Ответ — сам файл: новый документ — 201,
// повтор с тем же Idempotency-Key — 200 и тот же документ. Каждый запрос пишется в журнал.
func generateDocumentHandler(svc DocumentService, journal JournalStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		client, ok := clientFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		entry := db.CreateGenerationLogParams{
			APIClientID: client.ID,
			Project:     client.Project,
			RequestID:   middleware.GetReqID(r.Context()),
		}
		if key := r.Header.Get(idempotencyKeyHeader); key != "" {
			entry.IdempotencyKey = &key
		}
		defer func() {
			entry.DurationMs = int32(min(time.Since(started).Milliseconds(), int64(^uint32(0)>>1))) //nolint:gosec // ограничено выше
			recordGeneration(context.WithoutCancel(r.Context()), journal, entry)
		}()

		fail := func(e *apiError) {
			e.write(w, r)
			entry.HTTPStatus = int32(e.status) //nolint:gosec // HTTP-статус
			entry.Status = "rejected"
			if e.status >= 500 {
				entry.Status = "failed"
			}
			entry.ErrorCode, entry.ErrorMessage = &e.code, &e.message
			entry.ErrorDetails = jsonDetails(e.details)
		}

		form, formErr := readForm(w, r, generateFormLimits)
		if formErr != nil {
			fail(formErr)
			return
		}
		req := documents.DocxRequest{
			Template:       form["template"],
			Data:           form["data"],
			Format:         string(form["format"]),
			TemplateName:   string(form["template_name"]),
			ExternalID:     string(form["external_id"]),
			RequestedBy:    string(form["requested_by"]),
			IdempotencyKey: r.Header.Get(idempotencyKeyHeader),
		}
		entry.TemplateName, entry.ExternalID, entry.RequestedBy = optional(req.TemplateName), optional(req.ExternalID), optional(req.RequestedBy)
		entry.Format = optional(req.Format)
		if len(req.Template) > 0 {
			sum := sha256Sum(req.Template)
			entry.TemplateSha256 = sum
		}
		if v := string(form["retention_days"]); v != "" {
			days, err := strconv.Atoi(v)
			if err != nil {
				fail(&apiError{status: http.StatusBadRequest, code: "invalid_retention_days", message: "retention_days must be a whole number of days"})
				return
			}
			req.RetentionDays = days
		}

		gen, err := svc.GenerateDOCX(r.Context(), client, req, middleware.GetReqID(r.Context()))
		if err != nil {
			fail(documentError(r, err))
			return
		}

		status, outcome := http.StatusOK, "repeated"
		if gen.Created {
			status, outcome = http.StatusCreated, "created"
		}
		entry.Status, entry.HTTPStatus = outcome, int32(status) //nolint:gosec // HTTP-статус
		entry.DocumentID = &gen.Document.ID
		size := int64(len(gen.File))
		entry.ResultSize = &size

		doc := gen.Document
		w.Header().Set("Location", "/v1/documents/"+doc.ID.String())
		w.Header().Set("X-Document-Id", doc.ID.String())
		w.Header().Set("X-Document-SHA256", hex.EncodeToString(doc.FileSha256))
		w.Header().Set("X-Document-Expires-At", doc.ExpiresAt.UTC().Format(time.RFC3339))
		writeFile(w, r, status, doc.Format, fmt.Sprintf("%s.%s", doc.PublicID, doc.Format), gen.File)
	}
}

// checkTemplateHandler — POST /v1/templates/check: проверка шаблона без генерации.
func checkTemplateHandler(svc DocumentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		form, formErr := readForm(w, r, map[string]int64{"template": docx.MaxFileSize})
		if formErr != nil {
			formErr.write(w, r)
			return
		}
		vars, err := svc.CheckTemplate(form["template"])
		if err != nil {
			documentError(r, err).write(w, r)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"valid": true, "variables": vars})
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
			documentError(r, err).write(w, r)
			return
		}

		writeJSON(w, r, http.StatusOK, toDocumentResponse(doc))
	}
}

// documentFileHandler отдаёт файл документа повторно.
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
			documentError(r, err).write(w, r)
			return
		}

		writeFile(w, r, http.StatusOK, doc.Format, fmt.Sprintf("%s.%s", doc.PublicID, doc.Format), content)
	}
}

func writeFile(w http.ResponseWriter, r *http.Request, status int, format, filename string, content []byte) {
	w.Header().Set("Content-Type", documents.ContentType(format))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if _, err := w.Write(content); err != nil { //nolint:gosec // файл документа с attachment и nosniff: браузер не выполнит его как HTML
		slog.ErrorContext(r.Context(), "write document file", "err", err)
	}
}

// documentError переводит ошибки сервиса документов в ответы API.
func documentError(r *http.Request, err error) *apiError {
	var (
		vErr *documents.ValidationError
		tErr *docx.TemplateError
		dErr *docx.DataError
	)
	switch {
	case errors.As(err, &vErr):
		return &apiError{status: http.StatusBadRequest, code: "invalid_" + vErr.Field, message: vErr.Error()}
	case errors.As(err, &tErr):
		return &apiError{status: http.StatusUnprocessableEntity, code: "invalid_template", message: "template cannot be used", details: tErr.Problems}
	case errors.As(err, &dErr):
		return &apiError{status: http.StatusBadRequest, code: "invalid_data", message: "data does not match the template", details: dErr.Fields}
	case errors.Is(err, documents.ErrIdempotencyConflict):
		return &apiError{status: http.StatusConflict, code: "idempotency_conflict", message: documents.ErrIdempotencyConflict.Error()}
	case errors.Is(err, documents.ErrDocumentNotFound):
		return &apiError{status: http.StatusNotFound, code: "document_not_found", message: documents.ErrDocumentNotFound.Error()}
	case errors.Is(err, documents.ErrDocumentExpired):
		return &apiError{status: http.StatusGone, code: "document_expired", message: documents.ErrDocumentExpired.Error()}
	case errors.Is(err, gotenberg.ErrConvert):
		return &apiError{status: http.StatusUnprocessableEntity, code: "conversion_failed", message: "the document could not be converted to pdf"}
	case errors.Is(err, gotenberg.ErrUnavailable):
		slog.WarnContext(r.Context(), "pdf converter unavailable", "err", err)
		return &apiError{status: http.StatusServiceUnavailable, code: "converter_unavailable", message: "pdf converter is unavailable, retry later"}
	default:
		slog.ErrorContext(r.Context(), "document operation", "err", err)
		return &apiError{status: http.StatusInternalServerError, code: "internal", message: "internal server error"}
	}
}

// recordGeneration пишет запись журнала. Сбой журнала не должен ломать выпуск документа:
// только логируем.
func recordGeneration(ctx context.Context, journal JournalStore, entry db.CreateGenerationLogParams) {
	if journal == nil {
		return
	}
	if err := journal.CreateGenerationLog(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "write generation journal", "err", err, "request_id", entry.RequestID)
	}
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
