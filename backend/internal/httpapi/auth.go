package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"docgen/internal/apikey"
	"docgen/internal/db"
)

// apiKeyHeader — заголовок, в котором портал присылает API-ключ.
const apiKeyHeader = "X-API-Key" //nolint:gosec // имя заголовка, а не секрет

// APIClientStore — то, что middleware авторизации нужно от хранилища.
type APIClientStore interface {
	GetActiveAPIClientByKeyHash(ctx context.Context, keyHash []byte) (db.APIClient, error)
}

// ctxKey — собственный тип ключей контекста, чтобы не пересечься с чужими пакетами.
type ctxKey int

const clientCtxKey ctxKey = 0

// requireAPIKey пропускает запрос дальше, только если в нём действующий API-ключ.
// Опознанный клиент кладётся в контекст запроса.
func requireAPIKey(store APIClientStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get(apiKeyHeader)
			if !strings.HasPrefix(key, apikey.Prefix) {
				writeUnauthorized(w, r)
				return
			}
			client, err := store.GetActiveAPIClientByKeyHash(r.Context(), apikey.Hash(key))
			if errors.Is(err, pgx.ErrNoRows) {
				writeUnauthorized(w, r)
				return
			}
			if err != nil {
				slog.ErrorContext(r.Context(), "failed to get client", "err", err)
				writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
				return
			}
			ctx := context.WithValue(r.Context(), clientCtxKey, client)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeUnauthorized(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusUnauthorized, "unauthorized", "missing or invalid API key")
}

// clientFromContext возвращает клиента, которого опознал requireAPIKey.
func clientFromContext(ctx context.Context) (db.APIClient, bool) {
	client, ok := ctx.Value(clientCtxKey).(db.APIClient)
	return client, ok
}
