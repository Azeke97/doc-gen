package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"docgen/internal/apikey"
	"docgen/internal/db"
	"docgen/internal/pgerr"
)

// maxKeyName — длина имени ключа: «Портал закупок, прод».
const maxKeyName = 200

// KeyStore — то, что обработчикам ключей нужно от хранилища.
type KeyStore interface {
	ListAPIClients(ctx context.Context, project *string) ([]db.ListAPIClientsRow, error)
	CreateAPIClient(ctx context.Context, arg db.CreateAPIClientParams) (db.APIClient, error)
	RevokeAPIClient(ctx context.Context, id uuid.UUID) (db.RevokeAPIClientRow, error)
	DeleteAPIClient(ctx context.Context, id uuid.UUID) (db.DeleteAPIClientRow, error)
	CreateAuditEntry(ctx context.Context, arg db.CreateAuditEntryParams) error
}

type keyResponse struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Project   string     `json:"project"`
	KeyPrefix string     `json:"key_prefix"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	// Used — по ключу есть документы или журнал: его можно только отозвать. Только в списке.
	Used *bool `json:"used,omitempty"`
}

// issuedKeyResponse — ключ целиком показывается только в ответе на выдачу.
type issuedKeyResponse struct {
	keyResponse
	Key string `json:"key"`
}

func listKeysHandler(store KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project := chi.URLParam(r, "code")
		rows, err := store.ListAPIClients(r.Context(), &project)
		if err != nil {
			slog.ErrorContext(r.Context(), "list api keys", "err", err)
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		keys := make([]keyResponse, 0, len(rows))
		for _, k := range rows {
			keys = append(keys, keyResponse{ID: k.ID, Name: k.Name, Project: k.Project, KeyPrefix: k.KeyPrefix, CreatedAt: k.CreatedAt, RevokedAt: k.RevokedAt, Used: &k.Used})
		}
		writeJSON(w, r, http.StatusOK, keys)
	}
}

// issueKeyHandler — POST /admin/v1/projects/{code}/keys: новый ключ портала. Сам ключ
// возвращается один раз, в базе остаются только хэш и начало.
func issueKeyHandler(store KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" || len([]rune(req.Name)) > maxKeyName {
			writeError(w, r, http.StatusBadRequest, "invalid_name", "name is required and must not exceed 200 characters")
			return
		}

		key := apikey.Generate()
		client, err := store.CreateAPIClient(r.Context(), db.CreateAPIClientParams{
			Name:      req.Name,
			Project:   chi.URLParam(r, "code"),
			KeyHash:   apikey.Hash(key),
			KeyPrefix: apikey.DisplayPrefix(key),
		})
		if pgerr.IsForeignKeyViolation(err) {
			writeError(w, r, http.StatusNotFound, "project_not_found", "project not found")
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "issue api key", "err", err)
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		audit(r.Context(), store, "api_key.issue", client.ID, client.Project, client.Name, client.KeyPrefix)
		writeJSON(w, r, http.StatusCreated, issuedKeyResponse{
			keyResponse: keyResponse{ID: client.ID, Name: client.Name, Project: client.Project, KeyPrefix: client.KeyPrefix, CreatedAt: client.CreatedAt},
			Key:         key,
		})
	}
}

// revokeKeyHandler — POST /admin/v1/keys/{id}/revoke: портал с этим ключом сразу получает 401.
// Строка ключа остаётся: на неё ссылаются документы и журнал.
func revokeKeyHandler(store KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}

		k, err := store.RevokeAPIClient(r.Context(), id)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "key_not_found", "key not found or already revoked")
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "revoke api key", "err", err)
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		audit(r.Context(), store, "api_key.revoke", k.ID, k.Project, k.Name, k.KeyPrefix)
		writeJSON(w, r, http.StatusOK, keyResponse{ID: k.ID, Name: k.Name, Project: k.Project, KeyPrefix: k.KeyPrefix, CreatedAt: k.CreatedAt, RevokedAt: k.RevokedAt})
	}
}

// deleteKeyHandler — DELETE /admin/v1/keys/{id}: убрать отозванный ключ, которым ничего
// не выпускали (например, выданный по ошибке). Ключ с документами или журналом остаётся навсегда.
func deleteKeyHandler(store KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}

		k, err := store.DeleteAPIClient(r.Context(), id)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "key_not_found", "key not found or not revoked")
			return
		}
		if pgerr.IsForeignKeyViolation(err) {
			writeError(w, r, http.StatusConflict, "key_in_use", "key has documents or journal entries; it can only be revoked")
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "delete api key", "err", err)
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		audit(r.Context(), store, "api_key.delete", k.ID, k.Project, k.Name, k.KeyPrefix)
		w.WriteHeader(http.StatusNoContent)
	}
}

// audit записывает действие с ключом в audit_log. Ошибка записи только логируется:
// ключ уже выдан или отозван, откатывать его из-за аудита хуже.
func audit(ctx context.Context, store KeyStore, action string, id uuid.UUID, project, name, prefix string) {
	actor, _ := actorFromContext(ctx)
	details, _ := json.Marshal(map[string]string{"project": project, "name": name, "key_prefix": prefix})
	err := store.CreateAuditEntry(ctx, db.CreateAuditEntryParams{
		Actor: actor.Email, Action: action, EntityType: "api_client", EntityID: id, Details: details,
	})
	if err != nil {
		slog.ErrorContext(ctx, "write audit entry", "action", action, "err", err)
	}
	slog.InfoContext(ctx, action, "project", project, "key_prefix", prefix, "actor", actor.Email)
}
