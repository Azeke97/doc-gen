package documents

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"

	"docgen/internal/db"
)

var publicIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Verify находит документ по public_id для публичной страницы проверки. Ключ не нужен:
// public_id не угадать, а ответ не раскрывает содержимое документа. Неверный формат
// идентификатора — тот же «не найден», что и неизвестный.
func (s *Service) Verify(ctx context.Context, publicID string) (db.GetDocumentByPublicIDRow, error) {
	if !publicIDPattern.MatchString(publicID) {
		return db.GetDocumentByPublicIDRow{}, ErrDocumentNotFound
	}

	doc, err := db.New(s.db).GetDocumentByPublicID(ctx, publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.GetDocumentByPublicIDRow{}, ErrDocumentNotFound
	}
	if err != nil {
		return db.GetDocumentByPublicIDRow{}, fmt.Errorf("verify document: %w", err)
	}
	if !s.now().Before(doc.ExpiresAt) {
		return db.GetDocumentByPublicIDRow{}, ErrDocumentExpired
	}
	return doc, nil
}
