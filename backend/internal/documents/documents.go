// Package documents выпускает и хранит документы, сгенерированные по шаблонам.
package documents

import (
	"crypto/rand"
	"encoding/hex"
)

// NewPublicID возвращает публичный идентификатор документа: 32 hex-символа (128 случайных бит).
// Он печатается в колонтитуле и QR-коде и открывает страницу проверки, поэтому не должен
// угадываться и не раскрывает, когда документ создан (в отличие от id, это UUIDv7).
func NewPublicID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
