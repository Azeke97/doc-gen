package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"docgen/internal/db"
	"docgen/internal/docx"
	"docgen/internal/pgerr"
)

// Пределы полей запроса (как CHECK в миграциях).
const (
	MaxTemplateName = 200
	MaxExternalID   = 255
	MaxRequestedBy  = 255
	// MaxData — предел JSON с данными.
	MaxData = 5 << 20
)

// Типы файлов результата.
const (
	ContentTypePDF  = "application/pdf"
	ContentTypeDOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
)

// ValidationError — неверное поле запроса (не шаблона и не данных).
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// DocxRequest — запрос портала на выпуск документа по DOCX-шаблону.
type DocxRequest struct {
	Template       []byte
	Data           []byte
	Format         string // docx или pdf
	TemplateName   string // как шаблон называет портал: по нему строятся отчёты
	ExternalID     string // сущность в портале, ради которой выпущен документ
	RequestedBy    string // ID пользователя в портале, не ФИО и не ИИН
	RetentionDays  int
	IdempotencyKey string
}

// Generated — выпущенный (или найденный по ключу идемпотентности) документ с файлом.
type Generated struct {
	Document db.Document
	File     []byte
	Created  bool // false — повтор запроса с тем же ключом
}

// ContentType — тип файла документа.
func ContentType(format string) string {
	if format == "docx" {
		return ContentTypeDOCX
	}
	return ContentTypePDF
}

// CheckTemplate проверяет шаблон без генерации и перечисляет поля, которые он использует.
// Ошибки шаблона — *docx.TemplateError.
func (s *Service) CheckTemplate(file []byte) ([]docx.Variable, error) {
	if len(file) == 0 {
		return nil, &ValidationError{Field: "template", Message: "is required"}
	}
	t, err := docx.Parse(file)
	if err != nil {
		return nil, err
	}
	return t.Variables(), nil
}

// GenerateDOCX выпускает документ: проверяет запрос, шаблон и данные, подставляет данные,
// при необходимости превращает DOCX в PDF, сохраняет файл, копию шаблона и строку документа.
// Повтор с тем же Idempotency-Key и тем же запросом возвращает уже выпущенный документ.
//
// Ошибки: *ValidationError (поля запроса), *docx.TemplateError, *docx.DataError,
// ErrIdempotencyConflict, gotenberg.ErrConvert и gotenberg.ErrUnavailable.
func (s *Service) GenerateDOCX(ctx context.Context, client db.APIClient, req DocxRequest, trace string) (Generated, error) {
	maxRetention, err := db.New(s.db).GetProjectRetention(ctx, client.Project)
	if err != nil {
		return Generated{}, fmt.Errorf("get project retention: %w", err)
	}
	if err := req.validate(maxRetention); err != nil {
		return Generated{}, err
	}

	templateSum := sha256.Sum256(req.Template)
	reqSum, data, err := req.fingerprint(templateSum)
	if err != nil {
		return Generated{}, err
	}

	if doc, found, err := s.existing(ctx, client, req.IdempotencyKey, reqSum); err != nil || found {
		if err != nil {
			return Generated{}, err
		}
		return s.repeat(ctx, client, doc)
	}

	tmpl, err := docx.Parse(req.Template)
	if err != nil {
		return Generated{}, err
	}
	file, err := tmpl.Render(data)
	if err != nil {
		return Generated{}, err
	}
	if req.Format == "pdf" {
		if file, err = s.pdf.ConvertDOCX(ctx, file, trace); err != nil {
			return Generated{}, err
		}
	}

	now := s.now()
	publicID := NewPublicID()
	key := fmt.Sprintf("%s/%s/%s.%s", client.Project, now.Format("2006/01"), publicID, req.Format)
	if err := s.files.Put(ctx, s.bucket, key, bytes.NewReader(file), int64(len(file)), ContentType(req.Format)); err != nil {
		return Generated{}, fmt.Errorf("store document file: %w", err)
	}

	fileSum := sha256.Sum256(file)
	var doc db.Document
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := db.New(tx)
		// Порядок важен: строка копии блокируется до конца транзакции, и только потом
		// файл копии кладётся в хранилище — так очистка не удалит его из-под документа.
		templateKey := hex.EncodeToString(templateSum[:]) + ".docx"
		if err := q.LockTemplateFile(ctx, db.LockTemplateFileParams{
			Sha256: templateSum[:], FileKey: templateKey, Size: int64(len(req.Template)),
		}); err != nil {
			return fmt.Errorf("save template copy: %w", err)
		}
		if err := s.files.Put(ctx, s.templateBucket, templateKey, bytes.NewReader(req.Template), int64(len(req.Template)), ContentTypeDOCX); err != nil {
			return fmt.Errorf("store template copy: %w", err)
		}

		doc, err = q.CreateDocxDocument(ctx, db.CreateDocxDocumentParams{
			PublicID:       publicID,
			APIClientID:    client.ID,
			Project:        client.Project,
			TemplateName:   &req.TemplateName,
			TemplateSha256: templateSum[:],
			ExternalID:     &req.ExternalID,
			RequestedBy:    &req.RequestedBy,
			Format:         req.Format,
			FileKey:        key,
			FileSha256:     fileSum[:],
			FileSize:       int64(len(file)),
			Data:           req.Data,
			IdempotencyKey: req.IdempotencyKey,
			RequestSha256:  reqSum,
			ExpiresAt:      now.Add(time.Duration(req.RetentionDays) * 24 * time.Hour),
		})
		return err
	})
	if err != nil {
		s.deleteFile(context.WithoutCancel(ctx), key)

		// Такой же запрос пришёл параллельно и успел первым — отдаём его документ.
		if pgerr.IsUniqueViolation(err) {
			doc, found, err := s.existing(ctx, client, req.IdempotencyKey, reqSum)
			if err == nil && found {
				return s.repeat(ctx, client, doc)
			}
			return Generated{}, err
		}
		return Generated{}, fmt.Errorf("create document: %w", err)
	}

	return Generated{Document: doc, File: file, Created: true}, nil
}

// repeat отдаёт уже выпущенный документ с его файлом.
func (s *Service) repeat(ctx context.Context, client db.APIClient, doc db.Document) (Generated, error) {
	_, file, err := s.File(ctx, client.Project, doc.ID)
	if err != nil {
		return Generated{}, err
	}
	return Generated{Document: doc, File: file}, nil
}

func (r *DocxRequest) validate(maxRetention int32) error {
	required := []struct {
		field, value string
		max          int
	}{
		{"idempotency_key", r.IdempotencyKey, MaxIdempotencyKey},
		{"template_name", r.TemplateName, MaxTemplateName},
		{"external_id", r.ExternalID, MaxExternalID},
		{"requested_by", r.RequestedBy, MaxRequestedBy},
	}
	for _, f := range required {
		if f.value == "" {
			return &ValidationError{Field: f.field, Message: "is required"}
		}
		if len(f.value) > f.max {
			return &ValidationError{Field: f.field, Message: fmt.Sprintf("must not exceed %d characters", f.max)}
		}
	}

	switch {
	case len(r.Template) == 0:
		return &ValidationError{Field: "template", Message: "is required"}
	case len(r.Template) > docx.MaxFileSize:
		return &ValidationError{Field: "template", Message: fmt.Sprintf("must not exceed %d bytes", docx.MaxFileSize)}
	case r.Format != "docx" && r.Format != "pdf":
		return &ValidationError{Field: "format", Message: "must be docx or pdf"}
	case len(r.Data) == 0:
		return &ValidationError{Field: "data", Message: "is required"}
	case len(r.Data) > MaxData:
		return &ValidationError{Field: "data", Message: fmt.Sprintf("must not exceed %d bytes", MaxData)}
	case r.RetentionDays < 1:
		return &ValidationError{Field: "retention_days", Message: "must be a positive number of days"}
	case r.RetentionDays > int(maxRetention):
		return &ValidationError{Field: "retention_days", Message: fmt.Sprintf("must not exceed %d days, the retention period of the project", maxRetention)}
	}
	return nil
}

// fingerprint — отпечаток запроса для идемпотентности и разобранные данные. Данные
// приводятся к одному виду (порядок ключей, пробелы), шаблон представлен своим SHA-256.
func (r *DocxRequest) fingerprint(templateSum [sha256.Size]byte) ([]byte, map[string]any, error) {
	data, err := docx.DecodeData(r.Data)
	if err != nil {
		return nil, nil, &ValidationError{Field: "data", Message: "must be a JSON object"}
	}
	canonical, err := json.Marshal(struct {
		Template      string         `json:"template_sha256"`
		Format        string         `json:"format"`
		TemplateName  string         `json:"template_name"`
		ExternalID    string         `json:"external_id"`
		RequestedBy   string         `json:"requested_by"`
		RetentionDays int            `json:"retention_days"`
		Data          map[string]any `json:"data"`
	}{hex.EncodeToString(templateSum[:]), r.Format, r.TemplateName, r.ExternalID, r.RequestedBy, r.RetentionDays, data})
	if err != nil {
		return nil, nil, fmt.Errorf("canonical request: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return sum[:], data, nil
}
