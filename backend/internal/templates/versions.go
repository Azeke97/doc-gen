package templates

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"docgen/internal/db"
)

// MaxFileSize — предельный размер файла шаблона.
const MaxFileSize = 10 << 20 // 10 МиБ

// UploadParams — содержимое новой версии шаблона.
type UploadParams struct {
	File     []byte
	Schema   json.RawMessage
	TestData json.RawMessage // необязательно
}

// UploadVersion загружает файл шаблона. Если у шаблона есть черновик, он заменяется,
// иначе создаётся новый черновик со следующим номером. created сообщает, что версия новая.
func (s *Service) UploadVersion(ctx context.Context, actor string, templateID uuid.UUID, p UploadParams) (db.TemplateVersion, bool, error) {
	var (
		version db.TemplateVersion
		created bool
	)

	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)

		tmpl, err := q.LockTemplate(ctx, templateID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTemplateNotFound
		}
		if err != nil {
			return err
		}
		if tmpl.Status == "archived" {
			return ErrTemplateArchived
		}

		if err := validateUpload(tmpl.Engine, p); err != nil {
			return err
		}

		draft, err := q.GetDraftVersion(ctx, tmpl.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		created = errors.Is(err, pgx.ErrNoRows)

		number := draft.Version
		if created {
			if number, err = q.NextVersionNumber(ctx, tmpl.ID); err != nil {
				return err
			}
		}

		key := fileKey(tmpl, number)
		sum := sha256.Sum256(p.File)
		if err := s.files.Put(ctx, s.bucket, key, bytes.NewReader(p.File), int64(len(p.File)), contentType(tmpl.Engine)); err != nil {
			return err
		}

		if created {
			version, err = q.CreateTemplateVersion(ctx, db.CreateTemplateVersionParams{
				TemplateID: tmpl.ID,
				Version:    number,
				FileKey:    key,
				FileSha256: sum[:],
				DataSchema: p.Schema,
				TestData:   p.TestData,
				CreatedBy:  actor,
			})
		} else {
			version, err = q.ReplaceDraftVersion(ctx, db.ReplaceDraftVersionParams{
				ID:         draft.ID,
				FileKey:    key,
				FileSha256: sum[:],
				DataSchema: p.Schema,
				TestData:   p.TestData,
				CreatedBy:  actor,
			})
		}
		if err != nil {
			return err
		}

		return writeAudit(ctx, q, actor, ActionVersionUploaded, entityTemplateVersion, version.ID, map[string]any{
			"template_id": tmpl.ID,
			"version":     version.Version,
			"sha256":      hex.EncodeToString(sum[:]),
			"replaced":    !created,
		})
	})
	if err != nil {
		return db.TemplateVersion{}, false, fmt.Errorf("upload version: %w", err)
	}

	return version, created, nil
}

// ListVersions возвращает версии шаблона, новые первыми.
func (s *Service) ListVersions(ctx context.Context, templateID uuid.UUID) ([]db.TemplateVersion, error) {
	list, err := db.New(s.pool).ListTemplateVersions(ctx, templateID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}

	return list, nil
}

// validateUpload проверяет файл и схему до записи в хранилище.
func validateUpload(engine string, p UploadParams) error {
	switch {
	case len(p.File) == 0:
		return &ValidationError{Field: "file", Message: "is required"}
	case len(p.File) > MaxFileSize:
		return &ValidationError{Field: "file", Message: fmt.Sprintf("must not exceed %d bytes", MaxFileSize)}
	case !isJSONObject(p.Schema):
		return &ValidationError{Field: "schema", Message: "must be a JSON object"}
	case len(p.TestData) > 0 && !json.Valid(p.TestData):
		return &ValidationError{Field: "test_data", Message: "must be valid JSON"}
	}

	switch engine {
	case "html":
		// Проверяем, что шаблон разбирается. Когда появятся хелперы (этап 3),
		// сюда передадим тот же набор функций, что и при генерации.
		if _, err := template.New("check").Parse(string(p.File)); err != nil {
			return &ValidationError{Field: "file", Message: "invalid HTML template: " + err.Error()}
		}
	case "docx":
		// DOCX — это zip-архив; полная проверка появится вместе с DOCX-движком.
		if !bytes.HasPrefix(p.File, []byte("PK\x03\x04")) {
			return &ValidationError{Field: "file", Message: "must be a .docx file"}
		}
	}

	return nil
}

func isJSONObject(raw json.RawMessage) bool {
	var obj map[string]any
	return json.Unmarshal(raw, &obj) == nil && obj != nil
}

// fileKey — путь файла версии в бакете: <id шаблона>/v<номер>.<расширение>.
func fileKey(tmpl db.Template, version int32) string {
	return fmt.Sprintf("%s/v%d.%s", tmpl.ID, version, tmpl.Engine)
}

func contentType(engine string) string {
	if engine == "docx" {
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	}
	return "text/html; charset=utf-8"
}
