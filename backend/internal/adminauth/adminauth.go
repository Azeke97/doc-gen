// Package adminauth — пароли и токены сессий администраторов.
package adminauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"golang.org/x/crypto/bcrypt"
)

// SessionPrefix — префикс токенов сессий админки (у ключей порталов — dg_).
const SessionPrefix = "dgs_"

const (
	sessionTokenBytes = 32
	passwordBytes     = 18
)

// GenerateSessionToken создаёт случайный токен сессии.
func GenerateSessionToken() string {
	return SessionPrefix + randomString(sessionTokenBytes)
}

// GeneratePassword создаёт случайный пароль для нового администратора.
func GeneratePassword() string {
	return randomString(passwordBytes)
}

// HashToken возвращает SHA-256 токена сессии — то, что хранится в БД.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// HashPassword возвращает bcrypt-хэш пароля.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword сообщает, подходит ли пароль к хэшу.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
