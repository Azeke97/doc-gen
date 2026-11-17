package documents

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"docgen/internal/db"
	"docgen/internal/pgerr"
)

func TestNewPublicID(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{32}$`)
	seen := map[string]bool{}

	for range 1000 {
		id := NewPublicID()
		if !pattern.MatchString(id) {
			t.Fatalf("public id %q is not 32 hex chars", id)
		}
		if seen[id] {
			t.Fatalf("duplicate public id %q", id)
		}
		seen[id] = true
	}
}

// fixture — всё, на что ссылается документ: проект, ключ портала, шаблон и его версии.
type fixture struct {
	client    db.APIClient
	tmpl      db.Template
	published db.TemplateVersion
	draft     db.TemplateVersion
}

// newTestTx открывает транзакцию, которая откатывается в конце теста, и заполняет её данными.
func newTestTx(t *testing.T) (pgx.Tx, fixture) {
	t.Helper()

	if os.Getenv("DOCGEN_INTEGRATION") != "1" {
		t.Skip("set DOCGEN_INTEGRATION=1 to run integration tests")
	}
	ctx := t.Context()

	pool, err := pgxpool.New(ctx, os.Getenv("DB_URL"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	q := db.New(tx)
	project := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := q.CreateProject(ctx, db.CreateProjectParams{Code: project, Name: "Test"}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	var f fixture
	if f.client, err = q.CreateAPIClient(ctx, db.CreateAPIClientParams{
		Name: "Test portal", Project: project, KeyHash: []byte(project), KeyPrefix: "dg_test",
	}); err != nil {
		t.Fatalf("create client: %v", err)
	}
	if f.tmpl, err = q.CreateTemplate(ctx, db.CreateTemplateParams{
		Project: project, Code: "opening_protocol", Locale: "bilingual", Name: "Протокол", Engine: "html", RetentionDays: 30,
	}); err != nil {
		t.Fatalf("create template: %v", err)
	}

	newVersion := func(n int32) db.TemplateVersion {
		v, err := q.CreateTemplateVersion(ctx, db.CreateTemplateVersionParams{
			TemplateID: f.tmpl.ID, Version: n, FileKey: "test/key", FileSha256: make([]byte, 32),
			DataSchema: json.RawMessage(`{"type":"object"}`), CreatedBy: "test",
		})
		if err != nil {
			t.Fatalf("create version: %v", err)
		}
		return v
	}
	f.published = newVersion(1)
	by := "test"
	if f.published, err = q.PublishVersion(ctx, db.PublishVersionParams{ID: f.published.ID, PublishedBy: &by}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	f.draft = newVersion(2)

	return tx, f
}

func documentParams(f fixture, v db.TemplateVersion, key string) db.CreateDocumentParams {
	sum := sha256.Sum256([]byte(key))
	return db.CreateDocumentParams{
		PublicID:          NewPublicID(),
		APIClientID:       f.client.ID,
		Project:           f.client.Project,
		TemplateID:        &f.tmpl.ID,
		TemplateVersionID: &v.ID,
		Format:            "pdf",
		FileKey:           "documents/test.pdf",
		FileSha256:        sum[:],
		FileSize:          1024,
		Data:              json.RawMessage(`{"number":"0001"}`),
		IdempotencyKey:    key,
		RequestSha256:     sum[:],
		ExpiresAt:         time.Now().Add(30 * 24 * time.Hour),
	}
}

// savepoint выполняет fn во вложенной транзакции: ошибка внутри не ломает внешнюю транзакцию теста.
func savepoint(t *testing.T, tx pgx.Tx, fn func(sp pgx.Tx) error) error {
	t.Helper()

	sp, err := tx.Begin(t.Context())
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	defer func() { _ = sp.Rollback(t.Context()) }()

	return fn(sp)
}

func TestDocumentConstraints(t *testing.T) {
	tx, f := newTestTx(t)
	ctx := t.Context()
	q := db.New(tx)

	doc, err := q.CreateDocument(ctx, documentParams(f, f.published, "key-1"))
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}

	got, err := q.GetDocumentByIdempotencyKey(ctx, db.GetDocumentByIdempotencyKeyParams{APIClientID: f.client.ID, IdempotencyKey: "key-1"})
	if err != nil || got.ID != doc.ID {
		t.Errorf("by idempotency key = %v, %v; want %s", got.ID, err, doc.ID)
	}

	_, err = q.GetProjectDocument(ctx, db.GetProjectDocumentParams{ID: doc.ID, Project: "other_project"})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("document of another project: err = %v, want ErrNoRows", err)
	}

	tests := []struct {
		name string
		fn   func(sp pgx.Tx) error
		want func(error) bool
	}{
		{
			name: "same idempotency key",
			fn: func(sp pgx.Tx) error {
				_, err := db.New(sp).CreateDocument(ctx, documentParams(f, f.published, "key-1"))
				return err
			},
			want: pgerr.IsUniqueViolation,
		},
		{
			name: "draft version",
			fn: func(sp pgx.Tx) error {
				_, err := db.New(sp).CreateDocument(ctx, documentParams(f, f.draft, "key-2"))
				return err
			},
			want: errorContains("is not published"),
		},
		{
			name: "version of another template",
			fn: func(sp pgx.Tx) error {
				p := documentParams(f, f.published, "key-3")
				other := uuid.New()
				p.TemplateID = &other
				_, err := db.New(sp).CreateDocument(ctx, p)
				return err
			},
			want: pgerr.IsForeignKeyViolation,
		},
		{
			name: "update is forbidden",
			fn: func(sp pgx.Tx) error {
				_, err := sp.Exec(ctx, "UPDATE documents SET external_id = 'x' WHERE id = $1", doc.ID)
				return err
			},
			want: errorContains("cannot be changed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := savepoint(t, tx, tt.fn); !tt.want(err) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func errorContains(text string) func(error) bool {
	return func(err error) bool { return err != nil && strings.Contains(err.Error(), text) }
}
