// Package templates — реестр шаблонов: правила ТЗ (Ф1, Ф2, Ф16) поверх таблиц templates и template_versions.
package templates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"docgen/internal/dataschema"
	"docgen/internal/db"
	"docgen/internal/gotenberg"
	"docgen/internal/pgerr"
	"docgen/internal/storage"
)

// Действия в журнале аудита. Прошедшее время: журнал фиксирует то, что уже произошло.
const (
	ActionTemplateCreated   = "template.created"
	ActionTemplateActivated = "template.activated"
	ActionTemplateArchived  = "template.archived"
	ActionVersionUploaded   = "template_version.uploaded"
	ActionVersionPublished  = "template_version.published"
)

// Типы сущностей в журнале аудита.
const (
	entityTemplate        = "template"
	entityTemplateVersion = "template_version"
)

// Ошибки, которые обработчики превращают в понятные ответы клиенту.
var (
	ErrProjectNotFound  = errors.New("project not found")
	ErrTemplateExists   = errors.New("template with this project, code and locale already exists")
	ErrTemplateNotFound = errors.New("template not found")
	ErrTemplateArchived = errors.New("template is archived")

	ErrVersionNotFound         = errors.New("template version not found")
	ErrVersionAlreadyPublished = errors.New("template version is already published")
	ErrVersionNotPublished     = errors.New("template version is not published")
	ErrDraftChanged            = errors.New("draft was changed by someone else since you opened it")

	ErrNoTestData          = errors.New("template version has no test data")
	ErrPreviewNotSupported = errors.New("preview is available only for html templates")
	ErrRenderFailed        = errors.New("template cannot be rendered")
)

// ValidationError — данные не прошли проверку; Field — какое поле виновато.
// Errors — подробности по вложенным полям, если они есть (например, тестовые данные
// не подошли под схему).
type ValidationError struct {
	Field   string
	Message string
	Errors  []dataschema.FieldError
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// codePattern повторяет CHECK из миграции templates.
var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,99}$`)

var (
	locales = []string{"ru", "kk", "bilingual"}
	engines = []string{"docx", "html"}
)

// DB — то, через что сервис ходит в базу: умеет выполнять запросы и открывать транзакции.
// Ему соответствуют и *pgxpool.Pool (в работе), и pgx.Tx (в тестах: всё откатывается в конце).
type DB interface {
	db.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PDFConverter превращает HTML в PDF.
type PDFConverter interface {
	ConvertHTML(ctx context.Context, doc gotenberg.Document, trace string) ([]byte, error)
}

// Service выполняет операции с шаблонами. Каждая операция, меняющая данные,
// пишет запись в audit_log в той же транзакции.
type Service struct {
	pool   DB
	files  *storage.Store
	bucket string
	pdf    PDFConverter

	verifyBase string
}

// NewService создаёт сервис шаблонов. bucket — бакет, где лежат файлы шаблонов;
// verifyBase — адрес страницы проверки, к нему приписывается идентификатор документа.
func NewService(pool DB, files *storage.Store, bucket string, pdf PDFConverter, verifyBase string) *Service {
	return &Service{pool: pool, files: files, bucket: bucket, pdf: pdf, verifyBase: verifyBase}
}

// VerifyURL — ссылка на страницу проверки документа: её печатает {{verifyurl}} и кодирует {{qr}}.
func VerifyURL(base, documentID string) string {
	return strings.TrimRight(base, "/") + "/" + documentID
}

// CreateParams — данные нового шаблона.
type CreateParams struct {
	Project       string
	Code          string
	Locale        string
	Name          string
	Engine        string
	RetentionDays int32
}

func (p *CreateParams) validate() error {
	p.Name = strings.TrimSpace(p.Name)

	switch {
	case !codePattern.MatchString(p.Code):
		return &ValidationError{Field: "code", Message: "must start with a latin letter and contain only a-z, 0-9 and _ (2-100 chars)"}
	case !slices.Contains(locales, p.Locale):
		return &ValidationError{Field: "locale", Message: "must be one of " + strings.Join(locales, ", ")}
	case !slices.Contains(engines, p.Engine):
		return &ValidationError{Field: "engine", Message: "must be one of " + strings.Join(engines, ", ")}
	case p.Name == "":
		return &ValidationError{Field: "name", Message: "is required"}
	case p.RetentionDays <= 0:
		return &ValidationError{Field: "retention_days", Message: "must be greater than 0"}
	}
	return nil
}

// Create создаёт шаблон в статусе draft. actor — кто создаёт (попадает в аудит).
func (s *Service) Create(ctx context.Context, actor string, p CreateParams) (db.Template, error) {
	if err := p.validate(); err != nil {
		return db.Template{}, err
	}

	var tmpl db.Template
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)

		var err error
		tmpl, err = q.CreateTemplate(ctx, db.CreateTemplateParams{
			Project:       p.Project,
			Code:          p.Code,
			Locale:        p.Locale,
			Name:          p.Name,
			Engine:        p.Engine,
			RetentionDays: p.RetentionDays,
		})
		if err != nil {
			return err
		}

		return writeAudit(ctx, q, actor, ActionTemplateCreated, entityTemplate, tmpl.ID, map[string]any{
			"project": tmpl.Project,
			"code":    tmpl.Code,
			"locale":  tmpl.Locale,
		})
	})
	switch {
	case pgerr.IsForeignKeyViolation(err):
		return db.Template{}, ErrProjectNotFound
	case pgerr.IsUniqueViolation(err):
		return db.Template{}, ErrTemplateExists
	case err != nil:
		return db.Template{}, fmt.Errorf("create template: %w", err)
	}

	return tmpl, nil
}

// Get возвращает шаблон по id.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (db.Template, error) {
	tmpl, err := db.New(s.pool).GetTemplate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Template{}, ErrTemplateNotFound
	}
	if err != nil {
		return db.Template{}, fmt.Errorf("get template: %w", err)
	}
	return tmpl, nil
}

// List возвращает шаблоны проекта; пустой project — все шаблоны.
func (s *Service) List(ctx context.Context, project string) ([]db.Template, error) {
	var filter *string
	if project != "" {
		filter = &project
	}

	list, err := db.New(s.pool).ListTemplates(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}

	return list, nil
}

// writeAudit пишет запись журнала о действии с шаблоном.
func writeAudit(ctx context.Context, q *db.Queries, actor, action, entityType string, entityID uuid.UUID, details map[string]any) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal audit details: %w", err)
	}

	return q.CreateAuditEntry(ctx, db.CreateAuditEntryParams{
		Actor:      actor,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		Details:    raw,
	})
}
