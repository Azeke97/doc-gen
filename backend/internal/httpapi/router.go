package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Deps — зависимости HTTP-слоя, которые собирает main.
type Deps struct {
	Logger          *slog.Logger
	APIClients      APIClientStore
	AdminAuth       AdminAuthStore
	AdminSessionTTL time.Duration
	Projects        ProjectStore
	Documents       DocumentService
	Journal         JournalStore
	// AdminUI — собранная админка; nil — сервис её не раздаёт.
	AdminUI http.Handler
}

// NewRouter собирает все маршруты API.
func NewRouter(deps Deps) http.Handler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(requestLogger(deps.Logger))
	r.Use(middleware.Recoverer)

	r.Get("/healthz", healthHandler)

	r.Route("/v1", func(r chi.Router) {
		r.Use(requireAPIKey(deps.APIClients))

		r.Get("/whoami", whoamiHandler)
		r.Post("/documents", generateDocumentHandler(deps.Documents, deps.Journal))
		r.Post("/templates/check", checkTemplateHandler(deps.Documents))
		r.Get("/documents/{id}", getDocumentHandler(deps.Documents))
		r.Get("/documents/{id}/file", documentFileHandler(deps.Documents))
	})

	r.Route("/admin/v1", func(r chi.Router) {
		r.Post("/login", loginHandler(deps.AdminAuth, deps.AdminSessionTTL))

		r.Group(func(r chi.Router) {
			r.Use(requireAdmin(deps.AdminAuth))

			r.Post("/logout", logoutHandler(deps.AdminAuth))
			r.Get("/me", meHandler)

			r.Post("/projects", createProjectHandler(deps.Projects))
			r.Get("/projects", listProjectsHandler(deps.Projects))
		})
	})

	if deps.AdminUI != nil {
		r.Get("/admin", http.RedirectHandler("/admin/", http.StatusMovedPermanently).ServeHTTP)
		r.Handle("/admin/*", http.StripPrefix("/admin", deps.AdminUI))
	}

	return r
}
