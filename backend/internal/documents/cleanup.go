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

// DeleteOrphanTemplates удаляет до limit копий шаблонов, на которые не ссылается ни один
// документ: все документы по ним удалены по сроку хранения. В одной транзакции и в таком
// порядке: строки блокируются (копии, занятые выпуском документа, пропускаются), удаляются —
// внешний ключ не даст удалить копию, на которую успел сослаться новый документ, — и только
// потом удаляются файлы. Неудача с файлом не откатывает удаление: лишний файл безвреден.
func (s *Service) DeleteOrphanTemplates(ctx context.Context, limit int32) (int, error) {
	var keys []string

	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := db.New(tx)
		orphans, err := q.LockOrphanTemplateFiles(ctx, limit)
		if err != nil || len(orphans) == 0 {
			return err
		}
		shas := make([][]byte, 0, len(orphans))
		for _, o := range orphans {
			shas = append(shas, o.Sha256)
			keys = append(keys, o.FileKey)
		}
		if err := q.DeleteTemplateFiles(ctx, shas); err != nil {
			return err
		}
		for _, key := range keys {
			if err := s.files.Delete(ctx, s.templateBucket, key); err != nil {
				slog.WarnContext(ctx, "delete orphan template copy", "key", key, "err", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("delete orphan template copies: %w", err)
	}
	return len(keys), nil
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

// cleanup удаляет пачками просроченные документы, а затем ставшие ненужными копии шаблонов.
func (s *Service) cleanup(ctx context.Context, batch int32) {
	if n := drain(ctx, batch, s.DeleteExpired); n > 0 {
		slog.InfoContext(ctx, "expired documents deleted", "count", n)
	}
	if n := drain(ctx, batch, s.DeleteOrphanTemplates); n > 0 {
		slog.InfoContext(ctx, "orphan template copies deleted", "count", n)
	}
}

// drain повторяет step пачками, пока пачка не окажется неполной. Возвращает сколько удалено.
func drain(ctx context.Context, batch int32, step func(context.Context, int32) (int, error)) int {
	total := 0
	for {
		n, err := step(ctx, batch)
		if err != nil {
			if ctx.Err() == nil {
				slog.ErrorContext(ctx, "documents cleanup", "err", err)
			}
			return total
		}
		total += n
		if n < int(batch) {
			return total
		}
	}
}
