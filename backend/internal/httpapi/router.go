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
	Templates       TemplateService
	PortalTemplates PortalTemplateService
	Documents       DocumentService
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
		// Публичная проверка по QR-коду — без ключа.
		r.Get("/verify/{public_id}", verifyHandler(deps.Documents))

		r.Group(func(r chi.Router) {
			r.Use(requireAPIKey(deps.APIClients))

			r.Get("/whoami", whoamiHandler)
			r.Get("/templates", listPortalTemplatesHandler(deps.PortalTemplates))
			r.Get("/templates/{code}/schema", portalSchemaHandler(deps.PortalTemplates))
			r.Post("/documents", generateDocumentHandler(deps.Documents))
			r.Get("/documents/{id}", getDocumentHandler(deps.Documents))
			r.Get("/documents/{id}/file", documentFileHandler(deps.Documents))
		})
	})

	r.Route("/admin/v1", func(r chi.Router) {
		r.Post("/login", loginHandler(deps.AdminAuth, deps.AdminSessionTTL))

		r.Group(func(r chi.Router) {
			r.Use(requireAdmin(deps.AdminAuth))

			r.Post("/logout", logoutHandler(deps.AdminAuth))
			r.Get("/me", meHandler)

			r.Post("/projects", createProjectHandler(deps.Projects))
			r.Get("/projects", listProjectsHandler(deps.Projects))
			r.Post("/templates", createTemplateHandler(deps.Templates))
			r.Get("/templates", listTemplatesHandler(deps.Templates))
			r.Get("/templates/{id}", getTemplateHandler(deps.Templates))
			r.Post("/templates/{id}/versions", uploadVersionHandler(deps.Templates))
			r.Get("/templates/{id}/versions", listVersionsHandler(deps.Templates))
			r.Post("/templates/{id}/activate", activateTemplateHandler(deps.Templates))
			r.Post("/templates/{id}/archive", archiveTemplateHandler(deps.Templates))
			r.Get("/versions/{id}/diff", diffVersionHandler(deps.Templates))
			r.Get("/versions/{id}/file", versionFileHandler(deps.Templates))
			r.Get("/versions/{id}/preview", previewVersionHandler(deps.Templates))
			r.Post("/versions/{id}/publish", publishVersionHandler(deps.Templates))
		})
	})

	if deps.AdminUI != nil {
		r.Get("/admin", http.RedirectHandler("/admin/", http.StatusMovedPermanently).ServeHTTP)
		r.Handle("/admin/*", http.StripPrefix("/admin", deps.AdminUI))
	}

	return r
}
