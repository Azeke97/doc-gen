// Package apikey генерирует и хэширует API-ключи порталов.
package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// Prefix — префикс всех ключей DocGen.
const Prefix = "dg_"

const (
	// keyBytes — сколько случайных байт в ключе (256 бит).
	keyBytes = 32
	// displayPrefixLen — сколько первых символов ключа хранить для опознания.
	displayPrefixLen = 8
)

// Generate создаёт новый случайный API-ключ вида dg_<43 символа base64url>.
func Generate() string {
	b := make([]byte, keyBytes)
	_, _ = rand.Read(b)
	return Prefix + base64.RawURLEncoding.EncodeToString(b)
}

// Hash возвращает SHA-256 от ключа. Именно это значение хранится в БД.
func Hash(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}

// DisplayPrefix возвращает начало ключа, по которому его можно опознать в логах и админке.
func DisplayPrefix(key string) string {
	return key[:displayPrefixLen]
}
