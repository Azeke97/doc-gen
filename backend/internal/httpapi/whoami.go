package httpapi

import (
	"net/http"

	"github.com/google/uuid"
)

// whoamiResponse — ответ GET /v1/whoami.
type whoamiResponse struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Project string    `json:"project"`
}

func whoamiHandler(w http.ResponseWriter, r *http.Request) {
	client, ok := clientFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	writeJSON(w, r, http.StatusOK, whoamiResponse{
		ID:      client.ID,
		Name:    client.Name,
		Project: client.Project,
	})
}
