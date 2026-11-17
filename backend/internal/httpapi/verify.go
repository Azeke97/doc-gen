package httpapi

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"docgen/internal/db"
	"docgen/internal/documents"
	"docgen/internal/format"
)

//go:embed verify.html
var verifyPageSource string

var verifyPage = template.Must(template.New("verify").Parse(verifyPageSource))

// verifyResponse — ответ публичной проверки: подлинность и сведения о выпуске, без данных документа.
type verifyResponse struct {
	PublicID     string              `json:"public_id"`
	Valid        bool                `json:"valid"`
	TemplateName string              `json:"template_name"`
	ProjectName  string              `json:"project_name"`
	Format       string              `json:"format"`
	FileSHA256   string              `json:"file_sha256"`
	FileSize     int64               `json:"file_size"`
	CreatedAt    time.Time           `json:"created_at"`
	ExpiresAt    time.Time           `json:"expires_at"`
	Signatures   []signatureResponse `json:"signatures"`
}

// signatureResponse — подписант документа. Подписи (CMS) ещё не принимаются, список всегда пуст.
type signatureResponse struct{}

func toVerifyResponse(d db.GetDocumentByPublicIDRow) verifyResponse {
	return verifyResponse{
		PublicID:     d.PublicID,
		Valid:        true,
		TemplateName: d.TemplateName,
		ProjectName:  d.ProjectName,
		Format:       d.Format,
		FileSHA256:   hex.EncodeToString(d.FileSha256),
		FileSize:     d.FileSize,
		CreatedAt:    d.CreatedAt,
		ExpiresAt:    d.ExpiresAt,
		Signatures:   []signatureResponse{},
	}
}

// verifyPageData — то, что видит человек, открывший ссылку из QR-кода.
type verifyPageData struct {
	Status   string // valid, not_found, expired
	PublicID string
	Doc      verifyResponse
	Created  string
	Expires  string
}

// verifyHandler — GET /v1/verify/{public_id}, без ключа. По этой ссылке ведёт QR-код
// документа: браузеру отвечаем страницей, программам — JSON.
func verifyHandler(svc DocumentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		publicID := chi.URLParam(r, "public_id")
		doc, err := svc.Verify(r.Context(), publicID)

		w.Header().Set("Cache-Control", "no-store")
		// В адресе — public_id: не передаём его другим сайтам через Referer.
		w.Header().Set("Referrer-Policy", "no-referrer")

		if !wantsHTML(r) {
			if err != nil {
				writeDocumentError(w, r, err)
				return
			}
			writeJSON(w, r, http.StatusOK, toVerifyResponse(doc))
			return
		}

		data := verifyPageData{Status: "valid", PublicID: publicID}
		status := http.StatusOK
		switch {
		case err == nil:
			data.Doc = toVerifyResponse(doc)
			data.Created = doc.CreatedAt.In(format.Kazakhstan).Format("02.01.2006 15:04")
			data.Expires = doc.ExpiresAt.In(format.Kazakhstan).Format("02.01.2006")
		case errors.Is(err, documents.ErrDocumentNotFound):
			data.Status, status = "not_found", http.StatusNotFound
		case errors.Is(err, documents.ErrDocumentExpired):
			data.Status, status = "expired", http.StatusGone
		default:
			slog.ErrorContext(r.Context(), "verify document", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		writeVerifyPage(w, r, status, data)
	}
}

func writeVerifyPage(w http.ResponseWriter, r *http.Request, status int, data verifyPageData) {
	var buf bytes.Buffer
	if err := verifyPage.Execute(&buf, data); err != nil {
		slog.ErrorContext(r.Context(), "render verify page", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// wantsHTML — запрос пришёл из браузера (сканер QR открывает ссылку в нём).
func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}
