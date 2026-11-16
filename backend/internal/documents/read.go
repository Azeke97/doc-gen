package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"docgen/internal/db"
	"docgen/internal/gotenberg"
)

var (
	ErrDocumentNotFound = errors.New("document not found")
	ErrDocumentExpired  = errors.New("document retention period has expired")
)

// Get возвращает документ проекта. Документ с истёкшим сроком хранения уже недоступен,
// даже если очистка ещё не успела его удалить.
func (s *Service) Get(ctx context.Context, project string, id uuid.UUID) (db.GetProjectDocumentRow, error) {
	doc, err := db.New(s.db).GetProjectDocument(ctx, db.GetProjectDocumentParams{ID: id, Project: project})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.GetProjectDocumentRow{}, ErrDocumentNotFound
	}
	if err != nil {
		return db.GetProjectDocumentRow{}, fmt.Errorf("get document: %w", err)
	}
	if !s.now().Before(doc.Document.ExpiresAt) {
		return db.GetProjectDocumentRow{}, ErrDocumentExpired
	}
	return doc, nil
}

// File возвращает документ и его файл, сверенный с SHA-256 из базы.
func (s *Service) File(ctx context.Context, project string, id uuid.UUID) (db.GetProjectDocumentRow, []byte, error) {
	doc, err := s.Get(ctx, project, id)
	if err != nil {
		return db.GetProjectDocumentRow{}, nil, err
	}

	r, err := s.files.Get(ctx, s.bucket, doc.Document.FileKey)
	if err != nil {
		return db.GetProjectDocumentRow{}, nil, fmt.Errorf("load document file: %w", err)
	}
	defer func() { _ = r.Close() }()

	content, err := io.ReadAll(io.LimitReader(r, gotenberg.MaxPDF+1))
	if err != nil {
		return db.GetProjectDocumentRow{}, nil, fmt.Errorf("read document file: %w", err)
	}
	sum := sha256.Sum256(content)
	if !bytes.Equal(sum[:], doc.Document.FileSha256) {
		return db.GetProjectDocumentRow{}, nil, fmt.Errorf("document file %s does not match its sha256", doc.Document.FileKey)
	}

	return doc, content, nil
}
