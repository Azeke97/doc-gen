package templates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"docgen/internal/db"
	"docgen/internal/storage"
)

// newTestService подключается к настоящей БД из DB_URL. Только с DOCGEN_INTEGRATION=1.
// Для каждого теста создаётся свой проект, чтобы тесты не мешали друг другу и данным разработчика.
func newTestService(t *testing.T) (*Service, *pgxpool.Pool, string) {
	t.Helper()

	if os.Getenv("DOCGEN_INTEGRATION") != "1" {
		t.Skip("set DOCGEN_INTEGRATION=1 to run integration tests")
	}

	pool, err := pgxpool.New(t.Context(), os.Getenv("DB_URL"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	files, err := storage.New(storage.Config{
		Endpoint:  os.Getenv("S3_ENDPOINT"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		UseSSL:    os.Getenv("S3_USE_SSL") == "true",
		Region:    os.Getenv("S3_REGION"),
	})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	bucket := os.Getenv("S3_TEMPLATES_BUCKET")
	if err := files.EnsureBucket(t.Context(), bucket); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	project := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := db.New(pool).CreateProject(t.Context(), db.CreateProjectParams{Code: project, Name: "Test"}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	// После теста убираем всё, что он создал: файлы в хранилище, записи журнала, версии, шаблоны, проект.
	// Тесты не публикуют версии, поэтому черновики удаляются (опубликованные защищает триггер).
	t.Cleanup(func() {
		ctx := context.Background()

		rows, err := pool.Query(ctx,
			"SELECT v.file_key FROM template_versions v JOIN templates t ON t.id = v.template_id WHERE t.project = $1", project)
		if err != nil {
			t.Errorf("cleanup: %v", err)
			return
		}
		keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Errorf("cleanup: %v", err)
		}
		for _, key := range keys {
			_ = files.Delete(ctx, bucket, key)
		}

		for _, q := range []string{
			"DELETE FROM audit_log WHERE entity_id IN (SELECT id FROM templates WHERE project = $1)",
			"DELETE FROM audit_log WHERE entity_id IN (SELECT v.id FROM template_versions v JOIN templates t ON t.id = v.template_id WHERE t.project = $1)",
			"DELETE FROM template_versions WHERE template_id IN (SELECT id FROM templates WHERE project = $1)",
			"DELETE FROM templates WHERE project = $1",
			"DELETE FROM projects WHERE code = $1",
		} {
			if _, err := pool.Exec(ctx, q, project); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})

	return NewService(pool, files, bucket), pool, project
}

func validParams(project string) CreateParams {
	return CreateParams{
		Project:       project,
		Code:          "opening_protocol",
		Locale:        "bilingual",
		Name:          "Протокол вскрытия",
		Engine:        "html",
		RetentionDays: 1825,
	}
}

func TestCreateWritesAudit(t *testing.T) {
	svc, pool, project := newTestService(t)

	tmpl, err := svc.Create(t.Context(), "alice@example.com", validParams(project))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tmpl.Status != "draft" {
		t.Errorf("status = %q, want draft", tmpl.Status)
	}

	var actor, action string
	var details json.RawMessage
	err = pool.QueryRow(t.Context(),
		"SELECT actor, action, details FROM audit_log WHERE entity_id = $1", tmpl.ID,
	).Scan(&actor, &action, &details)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if actor != "alice@example.com" || action != "template.created" {
		t.Errorf("audit = %s %s, want alice@example.com template.created", actor, action)
	}
}

func TestCreateErrors(t *testing.T) {
	svc, _, project := newTestService(t)

	if _, err := svc.Create(t.Context(), "alice", validParams(project)); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	if _, err := svc.Create(t.Context(), "alice", validParams(project)); !errors.Is(err, ErrTemplateExists) {
		t.Errorf("duplicate: err = %v, want ErrTemplateExists", err)
	}

	if _, err := svc.Create(t.Context(), "alice", validParams("no_such_project")); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("unknown project: err = %v, want ErrProjectNotFound", err)
	}
}

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name      string
		change    func(p *CreateParams)
		wantField string
	}{
		{name: "bad code", change: func(p *CreateParams) { p.Code = "Протокол" }, wantField: "code"},
		{name: "bad locale", change: func(p *CreateParams) { p.Locale = "en" }, wantField: "locale"},
		{name: "bad engine", change: func(p *CreateParams) { p.Engine = "pdf" }, wantField: "engine"},
		{name: "empty name", change: func(p *CreateParams) { p.Name = "  " }, wantField: "name"},
		{name: "zero retention", change: func(p *CreateParams) { p.RetentionDays = 0 }, wantField: "retention_days"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validParams("any")
			tt.change(&p)

			var vErr *ValidationError
			if err := p.validate(); !errors.As(err, &vErr) || vErr.Field != tt.wantField {
				t.Errorf("err = %v, want ValidationError on %q", err, tt.wantField)
			}
		})
	}
}
