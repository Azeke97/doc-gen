package httpapi

import (
	"encoding/json"
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

// decodeJSON читает JSON-тело запроса в v. Неизвестные поля и лишние данные — ошибка.
// При ошибке сам отвечает клиенту 400 и возвращает false.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error())
		return false
	}
	if dec.More() {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must contain a single JSON object")
		return false
	}

	return true
}
