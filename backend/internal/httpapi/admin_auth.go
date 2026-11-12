package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"docgen/internal/adminauth"
	"docgen/internal/db"
)

// dummyPasswordHash сравнивается с паролем, когда пользователя нет: так ответ на неизвестный
// email занимает столько же времени, сколько на неверный пароль, и по времени не узнать, какие email есть.
const dummyPasswordHash = "$2a$10$dlACGuVDAZOJcMD0qXAYDOYPsDNLmQRSnN5u0XTMpCSsVXCmqOAJy" //nolint:gosec // хэш заведомо ненужного пароля, а не секрет

// AdminAuthStore — то, что входу в админку нужно от хранилища.
type AdminAuthStore interface {
	GetActiveAdminUserByEmail(ctx context.Context, email string) (db.AdminUser, error)
	CreateAdminSession(ctx context.Context, arg db.CreateAdminSessionParams) (db.AdminSession, error)
	GetAdminBySessionToken(ctx context.Context, tokenHash []byte) (db.GetAdminBySessionTokenRow, error)
	DeleteAdminSession(ctx context.Context, tokenHash []byte) error
}

// Actor — тот, кто выполняет действие в админке. Попадает в created_by, published_by и audit_log.
// Позже сюда добавятся роли (Roles), и маршруты получат проверку requireRole.
type Actor struct {
	ID    uuid.UUID
	Email string
	Name  string
}

const actorCtxKey ctxKey = 1

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type meResponse struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
}

func loginHandler(store AdminAuthStore, sessionTTL time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if !decodeJSON(w, r, &req) {
			return
		}

		user, err := store.GetActiveAdminUserByEmail(r.Context(), strings.ToLower(strings.TrimSpace(req.Email)))
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			slog.ErrorContext(r.Context(), "look up admin user", "err", err)
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		hash := user.PasswordHash
		if errors.Is(err, pgx.ErrNoRows) {
			hash = dummyPasswordHash
		}
		if !adminauth.CheckPassword(hash, req.Password) || errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
			return
		}

		token := adminauth.GenerateSessionToken()
		session, err := store.CreateAdminSession(r.Context(), db.CreateAdminSessionParams{
			UserID:    user.ID,
			TokenHash: adminauth.HashToken(token),
			ExpiresAt: time.Now().Add(sessionTTL),
		})
		if err != nil {
			slog.ErrorContext(r.Context(), "create admin session", "err", err)
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		slog.InfoContext(r.Context(), "admin logged in", "user_id", user.ID)
		writeJSON(w, r, http.StatusOK, loginResponse{Token: token, ExpiresAt: session.ExpiresAt})
	}
}

func logoutHandler(store AdminAuthStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, _ := bearerToken(r)
		if err := store.DeleteAdminSession(r.Context(), adminauth.HashToken(token)); err != nil {
			slog.ErrorContext(r.Context(), "delete admin session", "err", err)
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func meHandler(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	writeJSON(w, r, http.StatusOK, meResponse(actor))
}

// requireAdmin пропускает запрос, только если в нём действующий токен сессии администратора.
// Опознанный администратор кладётся в контекст запроса.
func requireAdmin(store AdminAuthStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok || !strings.HasPrefix(token, adminauth.SessionPrefix) {
				writeAdminUnauthorized(w, r)
				return
			}

			admin, err := store.GetAdminBySessionToken(r.Context(), adminauth.HashToken(token))
			if errors.Is(err, pgx.ErrNoRows) {
				writeAdminUnauthorized(w, r)
				return
			}
			if err != nil {
				slog.ErrorContext(r.Context(), "look up admin session", "err", err)
				writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
				return
			}

			actor := Actor{ID: admin.ID, Email: admin.Email, Name: admin.Name}
			ctx := context.WithValue(r.Context(), actorCtxKey, actor)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeAdminUnauthorized(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusUnauthorized, "unauthorized", "missing, invalid or expired session")
}

// bearerToken достаёт токен из заголовка Authorization: Bearer <токен>.
func bearerToken(r *http.Request) (string, bool) {
	return strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// actorFromContext возвращает того, кого опознал requireAdmin.
func actorFromContext(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorCtxKey).(Actor)
	return actor, ok
}
