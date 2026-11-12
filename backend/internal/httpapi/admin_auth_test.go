package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"docgen/internal/adminauth"
	"docgen/internal/db"
)

// fakeAdminAuthStore — подделка пользователей и сессий админки в памяти.
type fakeAdminAuthStore struct {
	users    map[string]db.AdminUser // по email
	sessions map[string]uuid.UUID    // хэш токена → id пользователя
}

func newFakeAdminAuthStore(t *testing.T, email, password string) *fakeAdminAuthStore {
	t.Helper()

	hash, err := adminauth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	user := db.AdminUser{ID: uuid.New(), Email: email, Name: "Test Admin", PasswordHash: hash}

	return &fakeAdminAuthStore{
		users:    map[string]db.AdminUser{email: user},
		sessions: map[string]uuid.UUID{},
	}
}

func (f *fakeAdminAuthStore) GetActiveAdminUserByEmail(_ context.Context, email string) (db.AdminUser, error) {
	user, ok := f.users[email]
	if !ok {
		return db.AdminUser{}, pgx.ErrNoRows
	}
	return user, nil
}

func (f *fakeAdminAuthStore) CreateAdminSession(_ context.Context, arg db.CreateAdminSessionParams) (db.AdminSession, error) {
	f.sessions[string(arg.TokenHash)] = arg.UserID
	return db.AdminSession{UserID: arg.UserID, TokenHash: arg.TokenHash, ExpiresAt: arg.ExpiresAt}, nil
}

func (f *fakeAdminAuthStore) GetAdminBySessionToken(_ context.Context, tokenHash []byte) (db.GetAdminBySessionTokenRow, error) {
	userID, ok := f.sessions[string(tokenHash)]
	if !ok {
		return db.GetAdminBySessionTokenRow{}, pgx.ErrNoRows
	}
	for _, u := range f.users {
		if u.ID == userID {
			return db.GetAdminBySessionTokenRow{ID: u.ID, Email: u.Email, Name: u.Name}, nil
		}
	}
	return db.GetAdminBySessionTokenRow{}, pgx.ErrNoRows
}

func (f *fakeAdminAuthStore) DeleteAdminSession(_ context.Context, tokenHash []byte) error {
	delete(f.sessions, string(tokenHash))
	return nil
}

// adminTestServer собирает роутер с подделками и умеет выполнять к нему запросы.
type adminTestServer struct {
	t         *testing.T
	auth      *fakeAdminAuthStore
	projects  *fakeProjectStore
	templates *fakeTemplateService
	router    http.Handler
}

func newAdminTestServer(t *testing.T) *adminTestServer {
	t.Helper()

	s := &adminTestServer{
		t:         t,
		auth:      newFakeAdminAuthStore(t, "admin@example.com", "secret-password"),
		projects:  &fakeProjectStore{projects: map[string]db.Project{}},
		templates: &fakeTemplateService{},
	}
	s.router = NewRouter(Deps{
		AdminAuth:       s.auth,
		AdminSessionTTL: time.Hour,
		Projects:        s.projects,
		Templates:       s.templates,
	})

	return s
}

func (s *adminTestServer) do(method, path, body, token string) *httptest.ResponseRecorder {
	s.t.Helper()

	req := httptest.NewRequestWithContext(s.t.Context(), method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	return rec
}

// login входит под тестовым администратором и возвращает токен сессии.
func (s *adminTestServer) login() string {
	s.t.Helper()

	rec := s.do(http.MethodPost, "/admin/v1/login", `{"email":"admin@example.com","password":"secret-password"}`, "")
	if rec.Code != http.StatusOK {
		s.t.Fatalf("login status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var body loginResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		s.t.Fatalf("decode login: %v", err)
	}

	return body.Token
}

func TestLogin(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "ok", body: `{"email":"admin@example.com","password":"secret-password"}`, wantStatus: http.StatusOK},
		{name: "email in other case", body: `{"email":" Admin@Example.com ","password":"secret-password"}`, wantStatus: http.StatusOK},
		{name: "wrong password", body: `{"email":"admin@example.com","password":"nope"}`, wantStatus: http.StatusUnauthorized},
		{name: "unknown email", body: `{"email":"ghost@example.com","password":"secret-password"}`, wantStatus: http.StatusUnauthorized},
		{name: "bad json", body: `{`, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newAdminTestServer(t)

			rec := s.do(http.MethodPost, "/admin/v1/login", tt.body, "")

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := newAdminTestServer(t)

	if rec := s.do(http.MethodGet, "/admin/v1/me", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me without token: status = %d, want 401", rec.Code)
	}

	token := s.login()

	rec := s.do(http.MethodGet, "/admin/v1/me", "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: status = %d, want 200", rec.Code)
	}
	var me meResponse
	if err := json.NewDecoder(rec.Body).Decode(&me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.Email != "admin@example.com" {
		t.Errorf("me.Email = %q, want admin@example.com", me.Email)
	}

	if rec := s.do(http.MethodPost, "/admin/v1/logout", "", token); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d, want 204", rec.Code)
	}
	if rec := s.do(http.MethodGet, "/admin/v1/me", "", token); rec.Code != http.StatusUnauthorized {
		t.Errorf("me after logout: status = %d, want 401", rec.Code)
	}
}
