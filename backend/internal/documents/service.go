package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"docgen/internal/dataschema"
	"docgen/internal/db"
	"docgen/internal/gotenberg"
	"docgen/internal/htmlengine"
	"docgen/internal/pgerr"
	"docgen/internal/storage"
	"docgen/internal/templates"
)

// MaxIdempotencyKey — предельная длина ключа идемпотентности (как CHECK в миграции).
const MaxIdempotencyKey = 255

// ErrIdempotencyConflict — ключ уже использован для другого запроса.
var ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")

// TemplateSource — то, что генерации нужно от реестра шаблонов.
type TemplateSource interface {
	Resolve(ctx context.Context, project string, ref templates.Ref) (db.ResolveTemplateVersionRow, error)
	LoadFile(ctx context.Context, key string, sha []byte) ([]byte, error)
}

// PDFConverter превращает HTML в PDF.
type PDFConverter interface {
	ConvertHTML(ctx context.Context, doc gotenberg.Document, trace string) ([]byte, error)
}

// DB — то, через что сервис ходит в базу: запросы и транзакции (пул или транзакция в тестах).
type DB interface {
	db.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service выпускает документы.
type Service struct {
	db        DB
	templates TemplateSource
	pdf       PDFConverter
	files     *storage.Store
	bucket    string
	now       func() time.Time

	verifyBase string
}

// NewService создаёт сервис документов. bucket — бакет, где лежат файлы документов;
// verifyBase — адрес страницы проверки, к нему приписывается public_id.
func NewService(conn DB, tmpl TemplateSource, pdf PDFConverter, files *storage.Store, bucket, verifyBase string) *Service {
	return &Service{db: conn, templates: tmpl, pdf: pdf, files: files, bucket: bucket, now: time.Now, verifyBase: verifyBase}
}

// GenerateRequest — запрос портала на выпуск документа.
type GenerateRequest struct {
	Template       string
	Locale         string
	Version        int32
	Format         string
	Data           json.RawMessage
	ExternalID     *string
	RequestedBy    *string
	IdempotencyKey string
}

// Generate выпускает документ. Повтор с тем же ключом идемпотентности возвращает
// уже выпущенный документ (created = false), а не создаёт новый.
func (s *Service) Generate(ctx context.Context, client db.APIClient, req GenerateRequest, trace string) (doc db.Document, created bool, err error) {
	if err := req.normalize(); err != nil {
		return db.Document{}, false, err
	}
	reqSum, err := req.sha256()
	if err != nil {
		return db.Document{}, false, err
	}

	if doc, found, err := s.existing(ctx, client, req.IdempotencyKey, reqSum); err != nil || found {
		return doc, false, err
	}

	version, err := s.templates.Resolve(ctx, client.Project, templates.Ref{Code: req.Template, Locale: req.Locale, Version: req.Version})
	if err != nil {
		return db.Document{}, false, err
	}
	if version.Engine != "html" || req.Format != "pdf" {
		return db.Document{}, false, &templates.ValidationError{
			Field: "format", Message: fmt.Sprintf("%s template cannot produce %s", version.Engine, req.Format),
		}
	}

	if err := validateData(version.DataSchema, req.Data); err != nil {
		return db.Document{}, false, err
	}

	publicID := NewPublicID()
	pdf, err := s.render(ctx, version, req.Data, publicID, trace)
	if err != nil {
		return db.Document{}, false, err
	}

	now := s.now()
	key := fmt.Sprintf("%s/%s/%s.pdf", client.Project, now.Format("2006/01"), publicID)
	if err := s.files.Put(ctx, s.bucket, key, bytes.NewReader(pdf), int64(len(pdf)), "application/pdf"); err != nil {
		return db.Document{}, false, err
	}

	fileSum := sha256.Sum256(pdf)
	doc, err = db.New(s.db).CreateDocument(ctx, db.CreateDocumentParams{
		PublicID:          publicID,
		APIClientID:       client.ID,
		Project:           client.Project,
		TemplateID:        &version.TemplateID,
		TemplateVersionID: &version.VersionID,
		ExternalID:        req.ExternalID,
		RequestedBy:       req.RequestedBy,
		Format:            req.Format,
		FileKey:           key,
		FileSha256:        fileSum[:],
		FileSize:          int64(len(pdf)),
		Data:              req.Data,
		IdempotencyKey:    req.IdempotencyKey,
		RequestSha256:     reqSum,
		ExpiresAt:         now.Add(time.Duration(version.RetentionDays) * 24 * time.Hour),
	})
	if err != nil {
		s.deleteFile(context.WithoutCancel(ctx), key)

		// Такой же запрос пришёл параллельно и успел первым — отдаём его документ.
		if pgerr.IsUniqueViolation(err) {
			doc, found, err := s.existing(ctx, client, req.IdempotencyKey, reqSum)
			if err == nil && found {
				return doc, false, nil
			}
			return db.Document{}, false, err
		}
		return db.Document{}, false, fmt.Errorf("create document: %w", err)
	}

	return doc, true, nil
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

// render отрисовывает версию на данных и превращает в PDF с колонтитулом документа.
func (s *Service) render(ctx context.Context, v db.ResolveTemplateVersionRow, data json.RawMessage, publicID, trace string) ([]byte, error) {
	src, err := s.templates.LoadFile(ctx, v.FileKey, v.FileSha256)
	if err != nil {
		return nil, err
	}

	page, err := htmlengine.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", templates.ErrRenderFailed, err)
	}
	html, err := page.Render(data, htmlengine.RenderOptions{
		DocumentID: publicID,
		VerifyURL:  templates.VerifyURL(s.verifyBase, publicID),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", templates.ErrRenderFailed, err)
	}

	pdf, err := s.pdf.ConvertHTML(ctx, gotenberg.Document{HTML: html, Footer: htmlengine.Footer(publicID)}, trace)
	if errors.Is(err, gotenberg.ErrConvert) {
		return nil, fmt.Errorf("%w: %w", templates.ErrRenderFailed, err)
	}
	if err != nil {
		return nil, fmt.Errorf("convert to pdf: %w", err)
	}
	return pdf, nil
}

func (s *Service) deleteFile(ctx context.Context, key string) {
	if err := s.files.Delete(ctx, s.bucket, key); err != nil {
		slog.WarnContext(ctx, "delete unreferenced document file", "key", key, "err", err)
	}
}

// validateData проверяет данные по схеме версии (ТЗ, Ф3): ошибки — по полям.
func validateData(schema, data json.RawMessage) error {
	compiled, err := dataschema.Compile(schema)
	if err != nil {
		return fmt.Errorf("compile schema of published version: %w", err)
	}
	fieldErrs, err := compiled.Validate(data)
	if err != nil {
		return &templates.ValidationError{Field: "data", Message: "must be a JSON object"}
	}
	if len(fieldErrs) > 0 {
		return &templates.ValidationError{Field: "data", Message: "does not match the template schema", Errors: fieldErrs}
	}
	return nil
}

func (r *GenerateRequest) normalize() error {
	if r.Format == "" {
		r.Format = "pdf"
	}
	switch {
	case r.IdempotencyKey == "":
		return &templates.ValidationError{Field: "idempotency_key", Message: "is required"}
	case len(r.IdempotencyKey) > MaxIdempotencyKey:
		return &templates.ValidationError{Field: "idempotency_key", Message: fmt.Sprintf("must not exceed %d characters", MaxIdempotencyKey)}
	case r.Template == "":
		return &templates.ValidationError{Field: "template", Message: "is required"}
	case r.Format != "pdf" && r.Format != "docx":
		return &templates.ValidationError{Field: "format", Message: "must be pdf or docx"}
	case len(r.Data) == 0:
		return &templates.ValidationError{Field: "data", Message: "is required"}
	}
	return nil
}

// sha256 — отпечаток запроса для проверки идемпотентности. Данные приводятся к одному виду
// (порядок ключей, пробелы), чтобы повтор того же запроса совпал побайтно.
func (r *GenerateRequest) sha256() ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(r.Data))
	dec.UseNumber()
	var data any
	if err := dec.Decode(&data); err != nil {
		return nil, &templates.ValidationError{Field: "data", Message: "must be valid JSON"}
	}

	canonical, err := json.Marshal(struct {
		Template    string  `json:"template"`
		Locale      string  `json:"locale"`
		Version     int32   `json:"version"`
		Format      string  `json:"format"`
		Data        any     `json:"data"`
		ExternalID  *string `json:"external_id"`
		RequestedBy *string `json:"requested_by"`
	}{r.Template, r.Locale, r.Version, r.Format, data, r.ExternalID, r.RequestedBy})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	sum := sha256.Sum256(canonical)
	return sum[:], nil
}
