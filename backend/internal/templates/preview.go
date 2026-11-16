package templates

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"docgen/internal/db"
	"docgen/internal/gotenberg"
	"docgen/internal/htmlengine"
)

// Preview отрисовывает версию на её тестовых данных и возвращает PDF с водяным знаком
// «образец». В колонтитуле вместо идентификатора документа — идентификатор версии.
// trace связывает запрос с логами Gotenberg.
func (s *Service) Preview(ctx context.Context, versionID uuid.UUID, trace string) ([]byte, error) {
	q := db.New(s.pool)

	version, err := q.GetTemplateVersion(ctx, versionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get version: %w", err)
	}

	tmpl, err := q.GetTemplate(ctx, version.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}
	if tmpl.Engine != "html" {
		return nil, ErrPreviewNotSupported
	}
	if len(version.TestData) == 0 {
		return nil, ErrNoTestData
	}

	src, err := s.LoadFile(ctx, version.FileKey, version.FileSha256)
	if err != nil {
		return nil, err
	}

	page, err := htmlengine.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRenderFailed, err)
	}
	docID := strings.ReplaceAll(version.ID.String(), "-", "")
	html, err := page.Render(version.TestData, htmlengine.RenderOptions{
		DocumentID: docID,
		VerifyURL:  VerifyURL(s.verifyBase, docID),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRenderFailed, err)
	}

	pdf, err := s.pdf.ConvertHTML(ctx, gotenberg.Document{
		HTML:   htmlengine.WithWatermark(html, htmlengine.PreviewWatermark),
		Footer: htmlengine.Footer(docID),
	}, trace)
	if errors.Is(err, gotenberg.ErrConvert) {
		return nil, fmt.Errorf("%w: %w", ErrRenderFailed, err)
	}
	if err != nil {
		return nil, fmt.Errorf("convert to pdf: %w", err)
	}

	return pdf, nil
}

// LoadFile читает файл версии из хранилища и сверяет его с SHA-256 из базы:
// подменённый в хранилище файл не должен попасть в документ.
func (s *Service) LoadFile(ctx context.Context, key string, sha []byte) ([]byte, error) {
	r, err := s.files.Get(ctx, s.bucket, key)
	if err != nil {
		return nil, fmt.Errorf("load template file: %w", err)
	}
	defer func() { _ = r.Close() }()

	content, err := io.ReadAll(io.LimitReader(r, MaxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read template file: %w", err)
	}

	sum := sha256.Sum256(content)
	if !bytes.Equal(sum[:], sha) {
		return nil, fmt.Errorf("template file %s does not match its sha256", key)
	}

	return content, nil
}
