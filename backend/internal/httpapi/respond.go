package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// errorResponse — единый формат ошибки во всех ответах API.
type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON отправляет v в формате JSON с указанным статусом.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.ErrorContext(r.Context(), "encode response", "err", err)
	}
}

// writeError отправляет ошибку в едином формате API.
func writeError(w http.ResponseWriter, r *http.Request, status int, code string, message string) {
	writeJSON(w, r, status, errorResponse{
		Error: errorBody{
			Code:    code,
			Message: message,
		},
	})
}
