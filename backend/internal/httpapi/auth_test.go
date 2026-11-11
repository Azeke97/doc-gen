package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"docgen/internal/apikey"
	"docgen/internal/db"
)

// fakeClientStore — подделка хранилища ключей: клиенты лежат в map по хэшу ключа.
// calls считает обращения, чтобы проверить, что до базы дело не дошло.
type fakeClientStore struct {
	clients map[string]db.APIClient
	err     error
	calls   int
}

func (f *fakeClientStore) GetActiveAPIClientByKeyHash(_ context.Context, keyHash []byte) (db.APIClient, error) {
	f.calls++

	if f.err != nil {
		return db.APIClient{}, f.err
	}

	client, ok := f.clients[string(keyHash)]
	if !ok {
		return db.APIClient{}, pgx.ErrNoRows
	}

	return client, nil
}

// doRequest выполняет GET-запрос к роутеру без настоящего сервера.
func doRequest(t *testing.T, store APIClientStore, path, key string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	if key != "" {
		req.Header.Set(apiKeyHeader, key)
	}

	rec := httptest.NewRecorder()
	NewRouter(Deps{APIClients: store}).ServeHTTP(rec, req)

	return rec
}

func TestRequireAPIKeyRejects(t *testing.T) {
	validKey := apikey.Generate()
	clients := map[string]db.APIClient{
		string(apikey.Hash(validKey)): {Name: "Портал закупок", Project: "procurement"},
	}

	tests := []struct {
		name          string
		storeErr      error
		key           string
		wantStatus    int
		wantCode      string
		wantStoreCall bool
	}{
		{name: "no key", key: "", wantStatus: http.StatusUnauthorized, wantCode: "unauthorized"},
		{name: "wrong prefix", key: "abc", wantStatus: http.StatusUnauthorized, wantCode: "unauthorized"},
		{
			name:          "unknown key",
			key:           apikey.Generate(),
			wantStatus:    http.StatusUnauthorized,
			wantCode:      "unauthorized",
			wantStoreCall: true,
		},
		{
			name:          "store failure",
			storeErr:      errors.New("connection refused"),
			key:           validKey,
			wantStatus:    http.StatusInternalServerError,
			wantCode:      "internal",
			wantStoreCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeClientStore{clients: clients, err: tt.storeErr}

			rec := doRequest(t, store, "/v1/whoami", tt.key)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			var body errorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Error.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", body.Error.Code, tt.wantCode)
			}
			if called := store.calls > 0; called != tt.wantStoreCall {
				t.Errorf("store called = %v, want %v", called, tt.wantStoreCall)
			}
		})
	}
}

func TestWhoami(t *testing.T) {
	key := apikey.Generate()
	client := db.APIClient{ID: uuid.New(), Name: "Портал закупок", Project: "procurement"}
	store := &fakeClientStore{clients: map[string]db.APIClient{string(apikey.Hash(key)): client}}

	rec := doRequest(t, store, "/v1/whoami", key)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body whoamiResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	want := whoamiResponse{ID: client.ID, Name: client.Name, Project: client.Project}
	if body != want {
		t.Errorf("body = %+v, want %+v", body, want)
	}
}

func TestHealthzDoesNotRequireKey(t *testing.T) {
	rec := doRequest(t, &fakeClientStore{}, "/healthz", "")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
