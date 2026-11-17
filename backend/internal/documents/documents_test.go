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

// fixture — всё, на что ссылается документ: проект, ключ портала и копия шаблона.
type fixture struct {
	client   db.APIClient
	template []byte // SHA-256 копии шаблона
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
	sum := sha256.Sum256([]byte(project))
	f.template = sum[:]
	if err := q.LockTemplateFile(ctx, db.LockTemplateFileParams{Sha256: f.template, FileKey: project + ".docx", Size: 100}); err != nil {
		t.Fatalf("template copy: %v", err)
	}

	return tx, f
}

func documentParams(f fixture, key string) db.CreateDocxDocumentParams {
	sum := sha256.Sum256([]byte(key))
	return db.CreateDocxDocumentParams{
		PublicID:       NewPublicID(),
		APIClientID:    f.client.ID,
		Project:        f.client.Project,
		TemplateName:   "opening_protocol",
		TemplateSha256: f.template,
		Format:         "pdf",
		FileKey:        "documents/test.pdf",
		FileSha256:     sum[:],
		FileSize:       1024,
		Data:           json.RawMessage(`{"number":"0001"}`),
		IdempotencyKey: key,
		RequestSha256:  sum[:],
		ExpiresAt:      time.Now().Add(30 * 24 * time.Hour),
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

	doc, err := q.CreateDocxDocument(ctx, documentParams(f, "key-1"))
	if err != nil {
		t.Fatalf("CreateDocxDocument: %v", err)
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
				_, err := db.New(sp).CreateDocxDocument(ctx, documentParams(f, "key-1"))
				return err
			},
			want: pgerr.IsUniqueViolation,
		},
		{
			name: "unknown template copy",
			fn: func(sp pgx.Tx) error {
				p := documentParams(f, "key-2")
				p.TemplateSha256 = make([]byte, 32)
				_, err := db.New(sp).CreateDocxDocument(ctx, p)
				return err
			},
			want: pgerr.IsForeignKeyViolation,
		},
		{
			name: "template copy in use cannot be deleted",
			fn: func(sp pgx.Tx) error {
				return db.New(sp).DeleteTemplateFiles(ctx, [][]byte{f.template})
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
