package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// maxJSONBody — предельный размер JSON-тела запроса.
const maxJSONBody = 1 << 20 // 1 МиБ

// errorResponse — единый формат ошибки во всех ответах API.
type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"` // подробности: ошибки по полям, изменения схемы
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

// writeErrorDetails — как writeError, но с подробностями в error.details.
func writeErrorDetails(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	writeJSON(w, r, status, errorResponse{
		Error: errorBody{
			Code:    code,
			Message: message,
			Details: details,
		},
	})
}

// decodeJSON читает JSON-тело запроса в v. Неизвестные поля и лишние данные — ошибка.
// При ошибке сам отвечает клиенту 400 и возвращает false.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeJSONLimit(w, r, v, maxJSONBody)
}

// decodeJSONLimit — как decodeJSON, но с собственным пределом размера тела; превышение — 413.
func decodeJSONLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, r, http.StatusRequestEntityTooLarge, "request_too_large", fmt.Sprintf("request body must not exceed %d bytes", limit))
			return false
		}
		writeError(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error())
		return false
	}
	if dec.More() {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must contain a single JSON object")
		return false
	}

	return true
}
