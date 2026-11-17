package documents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"docgen/internal/db"
	"docgen/internal/storage"
)

// MaxIdempotencyKey — предельная длина ключа идемпотентности (как CHECK в миграции).
const MaxIdempotencyKey = 255

// ErrIdempotencyConflict — ключ уже использован для другого запроса.
var ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")

// PDFConverter превращает DOCX в PDF.
type PDFConverter interface {
	ConvertDOCX(ctx context.Context, docx []byte, trace string) ([]byte, error)
}

// DB — то, через что сервис ходит в базу: запросы и транзакции (пул или транзакция в тестах).
type DB interface {
	db.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service выпускает и хранит документы.
type Service struct {
	db             DB
	pdf            PDFConverter
	files          *storage.Store
	bucket         string // файлы документов
	templateBucket string // копии шаблонов, по которым выпущены документы
	now            func() time.Time
}

// Buckets — бакеты хранилища, с которыми работает сервис.
type Buckets struct {
	Documents string // файлы документов
	Templates string // копии шаблонов из запросов
}

// NewService создаёт сервис документов.
func NewService(conn DB, pdf PDFConverter, files *storage.Store, buckets Buckets) *Service {
	return &Service{
		db: conn, pdf: pdf, files: files,
		bucket: buckets.Documents, templateBucket: buckets.Templates,
		now: time.Now,
	}
}

// existing ищет документ, уже выпущенный по этому ключу. Если ключ использован
// для другого запроса — ErrIdempotencyConflict.
func (s *Service) existing(ctx context.Context, client db.APIClient, key string, reqSum []byte) (db.Document, bool, error) {
	doc, err := db.New(s.db).GetDocumentByIdempotencyKey(ctx, db.GetDocumentByIdempotencyKeyParams{
		APIClientID: client.ID, IdempotencyKey: key,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Document{}, false, nil
	}
	if err != nil {
		return db.Document{}, false, fmt.Errorf("get document by idempotency key: %w", err)
	}
	if !bytes.Equal(doc.RequestSha256, reqSum) {
		return db.Document{}, false, ErrIdempotencyConflict
	}
	return doc, true, nil
}

func (s *Service) deleteFile(ctx context.Context, key string) {
	if err := s.files.Delete(ctx, s.bucket, key); err != nil {
		slog.WarnContext(ctx, "delete unreferenced document file", "key", key, "err", err)
	}
}
