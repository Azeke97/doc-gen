package documents

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"docgen/internal/db"
)

// DeleteExpired удаляет до limit документов с истёкшим сроком хранения (ТЗ, Ф10): файл,
// входные данные и строку целиком. Сначала файлы, потом строки: если файл удалить
// не удалось, строка остаётся и попадёт в следующую очистку.
func (s *Service) DeleteExpired(ctx context.Context, limit int32) (int, error) {
	var deleted int

	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := db.New(tx)

		expired, err := q.LockExpiredDocuments(ctx, db.LockExpiredDocumentsParams{ExpiresAt: s.now(), Limit: limit})
		if err != nil {
			return err
		}

		ids := make([]uuid.UUID, 0, len(expired))
		for _, d := range expired {
			if err := s.files.Delete(ctx, s.bucket, d.FileKey); err != nil {
				slog.WarnContext(ctx, "delete expired document file", "document_id", d.ID, "err", err)
				continue
			}
			ids = append(ids, d.ID)
		}
		if len(ids) == 0 {
			return nil
		}
		if err := q.DeleteDocuments(ctx, ids); err != nil {
			return err
		}
		deleted = len(ids)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("delete expired documents: %w", err)
	}

	return deleted, nil
}

// RunCleanup удаляет просроченные документы сразу и затем каждые interval, пока не отменён ctx.
func (s *Service) RunCleanup(ctx context.Context, interval time.Duration, batch int32) {
	for {
		s.cleanup(ctx, batch)

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// cleanup удаляет пачками, пока просроченные документы не кончатся.
func (s *Service) cleanup(ctx context.Context, batch int32) {
	total := 0
	for {
		n, err := s.DeleteExpired(ctx, batch)
		if err != nil {
			if ctx.Err() == nil {
				slog.ErrorContext(ctx, "documents cleanup", "err", err)
			}
			break
		}
		total += n
		if n < int(batch) {
			break
		}
	}
	if total > 0 {
		slog.InfoContext(ctx, "expired documents deleted", "count", total)
	}
}
