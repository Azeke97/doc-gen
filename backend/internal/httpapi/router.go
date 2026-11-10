package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Deps — зависимости HTTP-слоя, которые собирает main.
type Deps struct {
	APIClients APIClientStore
}

// NewRouter собирает все маршруты API.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", healthHandler)

	r.Route("/v1", func(r chi.Router) {
		r.Use(requireAPIKey(deps.APIClients))

		r.Get("/whoami", whoamiHandler)
	})

	return r
}
