package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// previewVersionHandler отдаёт PDF версии, отрисованной на её тестовых данных.
func previewVersionHandler(svc TemplateService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		versionID, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}

		pdf, err := svc.Preview(r.Context(), versionID, middleware.GetReqID(r.Context()))
		if err != nil {
			writeTemplateError(w, r, err)
			return
		}

		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="preview-%s.pdf"`, versionID))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(pdf); err != nil { //nolint:gosec // ответ — PDF с nosniff, браузер не выполнит его как HTML
			slog.ErrorContext(r.Context(), "write preview", "err", err)
		}
	}
}
