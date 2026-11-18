package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"docgen/internal/apikey"
	"docgen/internal/db"
)

// fakeKeyStore — ключи и записи аудита в памяти; проекты — test и other.
type fakeKeyStore struct {
	keys  []db.APIClient
	audit []db.CreateAuditEntryParams
}

func newFakeKeyStore() *fakeKeyStore { return &fakeKeyStore{} }

func (f *fakeKeyStore) ListAPIClients(_ context.Context, project *string) ([]db.ListAPIClientsRow, error) {
	var rows []db.ListAPIClientsRow
	for _, k := range f.keys {
		if project == nil || k.Project == *project {
			rows = append(rows, db.ListAPIClientsRow{ID: k.ID, Name: k.Name, Project: k.Project, KeyPrefix: k.KeyPrefix, CreatedAt: k.CreatedAt, RevokedAt: k.RevokedAt})
		}
	}
	return rows, nil
}

func (f *fakeKeyStore) CreateAPIClient(_ context.Context, arg db.CreateAPIClientParams) (db.APIClient, error) {
	if arg.Project != "test" && arg.Project != "other" {
		return db.APIClient{}, &pgconn.PgError{Code: "23503"}
	}
	k := db.APIClient{ID: uuid.Must(uuid.NewV7()), Name: arg.Name, Project: arg.Project, KeyHash: arg.KeyHash, KeyPrefix: arg.KeyPrefix, CreatedAt: time.Now()}
	f.keys = append(f.keys, k)
	return k, nil
}

func (f *fakeKeyStore) RevokeAPIClient(_ context.Context, id uuid.UUID) (db.RevokeAPIClientRow, error) {
	for i, k := range f.keys {
		if k.ID == id && k.RevokedAt == nil {
			now := time.Now()
			f.keys[i].RevokedAt = &now
			return db.RevokeAPIClientRow{ID: k.ID, Name: k.Name, Project: k.Project, KeyPrefix: k.KeyPrefix, CreatedAt: k.CreatedAt, RevokedAt: &now}, nil
		}
	}
	return db.RevokeAPIClientRow{}, pgx.ErrNoRows
}

func (f *fakeKeyStore) CreateAuditEntry(_ context.Context, arg db.CreateAuditEntryParams) error {
	f.audit = append(f.audit, arg)
	return nil
}

func TestKeysRequireSession(t *testing.T) {
	s := newAdminTestServer(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/admin/v1/projects/test/keys"},
		{http.MethodPost, "/admin/v1/projects/test/keys"},
		{http.MethodPost, "/admin/v1/keys/" + uuid.NewString() + "/revoke"},
	} {
		if rec := s.do(c.method, c.path, `{"name":"x"}`, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

func TestIssueListRevokeKey(t *testing.T) {
	s := newAdminTestServer(t)
	token := s.login()

	rec := s.do(http.MethodPost, "/admin/v1/projects/test/keys", `{"name":" Портал закупок "}`, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var issued struct {
		ID        uuid.UUID `json:"id"`
		Name      string    `json:"name"`
		Key       string    `json:"key"`
		KeyPrefix string    `json:"key_prefix"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(issued.Key, "dg_") || issued.KeyPrefix != apikey.DisplayPrefix(issued.Key) || issued.Name != "Портал закупок" {
		t.Errorf("issued = %+v", issued)
	}
	if got := s.keys.keys[0].KeyHash; !bytes.Equal(got, apikey.Hash(issued.Key)) {
		t.Error("stored hash does not match the issued key")
	}

	rec = s.do(http.MethodGet, "/admin/v1/projects/test/keys", "", token)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), issued.Key) || !strings.Contains(rec.Body.String(), issued.KeyPrefix) {
		t.Errorf("list: status = %d, body: %s (the key itself must not be listed)", rec.Code, rec.Body.String())
	}
	if rec := s.do(http.MethodGet, "/admin/v1/projects/other/keys", "", token); rec.Body.String() != "[]\n" {
		t.Errorf("other project keys = %s, want []", rec.Body.String())
	}

	revokePath := "/admin/v1/keys/" + issued.ID.String() + "/revoke"
	rec = s.do(http.MethodPost, revokePath, "", token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"revoked_at":"`) {
		t.Errorf("revoke: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if rec := s.do(http.MethodPost, revokePath, "", token); rec.Code != http.StatusNotFound {
		t.Errorf("second revoke: status = %d, want 404", rec.Code)
	}

	if len(s.keys.audit) != 2 || s.keys.audit[0].Action != "api_key.issue" || s.keys.audit[1].Action != "api_key.revoke" ||
		s.keys.audit[0].Actor != "admin@example.com" || s.keys.audit[0].EntityID != issued.ID {
		t.Errorf("audit = %+v", s.keys.audit)
	}
	if strings.Contains(string(s.keys.audit[0].Details), issued.Key) {
		t.Error("audit must not contain the key")
	}
}

func TestIssueKeyErrors(t *testing.T) {
	s := newAdminTestServer(t)
	token := s.login()

	tests := []struct {
		name, path, body string
		wantStatus       int
		wantCode         string
	}{
		{"empty name", "/admin/v1/projects/test/keys", `{"name":"  "}`, http.StatusBadRequest, "invalid_name"},
		{"long name", "/admin/v1/projects/test/keys", `{"name":"` + strings.Repeat("я", 201) + `"}`, http.StatusBadRequest, "invalid_name"},
		{"no project", "/admin/v1/projects/nope/keys", `{"name":"x"}`, http.StatusNotFound, "project_not_found"},
		{"bad id", "/admin/v1/keys/nope/revoke", "", http.StatusBadRequest, "invalid_id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := s.do(http.MethodPost, tt.path, tt.body, token)
			if rec.Code != tt.wantStatus || !strings.Contains(rec.Body.String(), `"`+tt.wantCode+`"`) {
				t.Errorf("status = %d, body: %s; want %d %s", rec.Code, rec.Body.String(), tt.wantStatus, tt.wantCode)
			}
		})
	}
	if len(s.keys.keys) != 0 || len(s.keys.audit) != 0 {
		t.Errorf("nothing must be stored: keys %d, audit %d", len(s.keys.keys), len(s.keys.audit))
	}
}
