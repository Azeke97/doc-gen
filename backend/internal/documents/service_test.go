package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"docgen/internal/db"
	"docgen/internal/gotenberg"
	"docgen/internal/storage"
	"docgen/internal/templates"
)

const testPage = `<!doctype html><html><head><meta charset="utf-8"><style>@page { size: A4 landscape; }</style></head>` +
	`<body><h1>Протокол № {{.number}}</h1><p>{{money .sum}}</p><p>{{verifyurl}}</p><img src="{{qr}}" alt="QR"></body></html>`

const testVerifyBase = "https://docgen.test/v1/verify/"

// testDocumentsBucket — отдельный бакет документов для тестов. Очистка ищет просроченные
// документы по всей базе, а тесты работают с базой разработчика: строки вернёт откат
// транзакции, а файлы из общего бакета удалились бы по-настоящему.
const testDocumentsBucket = "docgen-test-documents"

const testSchema = `{"type":"object","required":["number","sum"],"properties":{"number":{"type":"string"},"sum":{"type":"number"}}}`

// env — настоящие Postgres (в откатываемой транзакции), MinIO и Gotenberg.
type env struct {
	tx        pgx.Tx
	files     *storage.Store
	docBucket string
	templates *templates.Service
	documents *Service
	client    db.APIClient
	tmpl      db.Template
	v1        db.TemplateVersion
}

func newTestEnv(t *testing.T) *env {
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
	tmplBucket, docBucket := os.Getenv("S3_TEMPLATES_BUCKET"), testDocumentsBucket
	for _, b := range []string{tmplBucket, docBucket} {
		if err := files.EnsureBucket(ctx, b); err != nil {
			t.Fatalf("bucket %s: %v", b, err)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	project := fmt.Sprintf("test_%d", time.Now().UnixNano())

	// Хранилище транзакция не откатит: удаляем файлы этого проекта сами, потом откатываем базу.
	t.Cleanup(func() {
		ctx := context.Background()
		removeFiles(ctx, tx, files, tmplBucket,
			"SELECT v.file_key FROM template_versions v JOIN templates t ON t.id = v.template_id WHERE t.project = $1", project)
		removeFiles(ctx, tx, files, docBucket, "SELECT file_key FROM documents WHERE project = $1", project)
		removeFiles(ctx, tx, files, docBucket,
			"SELECT DISTINCT t.file_key FROM template_files t JOIN documents d ON d.template_sha256 = t.sha256 WHERE d.project = $1", project)
		if err := tx.Rollback(ctx); err != nil {
			t.Errorf("rollback: %v", err)
		}
	})

	q := db.New(tx)
	if _, err := q.CreateProject(ctx, db.CreateProjectParams{Code: project, Name: "Test"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	client, err := q.CreateAPIClient(ctx, db.CreateAPIClientParams{
		Name: "Test portal", Project: project, KeyHash: []byte(project), KeyPrefix: "dg_test",
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	pdf := gotenberg.New(os.Getenv("GOTENBERG_URL"), 30*time.Second)
	e := &env{
		tx:        tx,
		files:     files,
		docBucket: docBucket,
		templates: templates.NewService(tx, files, tmplBucket, pdf, testVerifyBase),
		client:    client,
	}
	e.documents = NewService(tx, e.templates, pdf, files, Buckets{Documents: docBucket, Templates: docBucket}, testVerifyBase)

	if e.tmpl, err = e.templates.Create(ctx, "alice", templates.CreateParams{
		Project: project, Code: "opening_protocol", Locale: "ru", Name: "Протокол", Engine: "html", RetentionDays: 30,
	}); err != nil {
		t.Fatalf("create template: %v", err)
	}
	e.v1 = e.publish(t, testSchema)

	return e
}

// publish загружает версию со схемой, публикует и делает активной.
func (e *env) publish(t *testing.T, schema string) db.TemplateVersion {
	t.Helper()
	ctx := t.Context()

	v, _, err := e.templates.UploadVersion(ctx, "alice", e.tmpl.ID, templates.UploadParams{File: []byte(testPage), Schema: json.RawMessage(schema)})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := e.templates.Publish(ctx, "bob", v.ID, templates.PublishOptions{ConfirmBreaking: true}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := e.templates.Activate(ctx, "bob", e.tmpl.ID, v.ID, templates.ActivateOptions{ConfirmBreaking: true}); err != nil {
		t.Fatalf("activate: %v", err)
	}
	return v
}

func removeFiles(ctx context.Context, tx pgx.Tx, files *storage.Store, bucket, query, project string) {
	rows, err := tx.Query(ctx, query, project)
	if err != nil {
		return
	}
	keys, _ := pgx.CollectRows(rows, pgx.RowTo[string])
	for _, key := range keys {
		_ = files.Delete(ctx, bucket, key)
	}
}

func request(key string) GenerateRequest {
	external := "lot-209092"
	return GenerateRequest{
		Template:       "opening_protocol",
		Data:           json.RawMessage(`{"number": "0001", "sum": 6000000.00}`),
		ExternalID:     &external,
		IdempotencyKey: key,
	}
}

func TestGenerate(t *testing.T) {
	e := newTestEnv(t)
	ctx := t.Context()

	doc, created, err := e.documents.Generate(ctx, e.client, request("key-1"), "test")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !created || *doc.TemplateVersionID != e.v1.ID || doc.Project != e.client.Project || doc.Format != "pdf" {
		t.Errorf("doc = %+v, created %v", doc, created)
	}
	if doc.ExternalID == nil || *doc.ExternalID != "lot-209092" {
		t.Errorf("external id = %v", doc.ExternalID)
	}
	if days := doc.ExpiresAt.Sub(doc.CreatedAt).Hours() / 24; days < 29.9 || days > 30.1 {
		t.Errorf("retention = %.1f days, want 30", days)
	}

	// Файл в хранилище — тот самый PDF, хэш и размер которого записаны в документе.
	r, err := e.files.Get(ctx, e.docBucket, doc.FileKey)
	if err != nil {
		t.Fatalf("get file: %v", err)
	}
	pdf, _ := io.ReadAll(r)
	_ = r.Close()
	sum := sha256.Sum256(pdf)
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || !bytes.Equal(sum[:], doc.FileSha256) || int64(len(pdf)) != doc.FileSize {
		t.Errorf("stored file does not match document: %d bytes, sha ok %v", len(pdf), bytes.Equal(sum[:], doc.FileSha256))
	}

	// Повтор того же запроса — тот же документ; порядок ключей и пробелы в данных не важны.
	again := request("key-1")
	again.Data = json.RawMessage(`{"sum":6000000.00,"number":"0001"}`)
	repeat, created, err := e.documents.Generate(ctx, e.client, again, "test")
	if err != nil || created || repeat.ID != doc.ID {
		t.Errorf("repeat = %s, created %v, %v; want %s, false", repeat.ID, created, err, doc.ID)
	}

	// Тот же ключ, другие данные — конфликт, а не чужой документ.
	other := request("key-1")
	other.Data = json.RawMessage(`{"number": "0002", "sum": 1}`)
	if _, _, err := e.documents.Generate(ctx, e.client, other, "test"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Errorf("reused key: err = %v, want ErrIdempotencyConflict", err)
	}
}

func TestGeneratePinnedVersion(t *testing.T) {
	e := newTestEnv(t)
	ctx := t.Context()

	v2 := e.publish(t, `{"type":"object","required":["number"],"properties":{"number":{"type":"string"},"sum":{"type":"number"}}}`)

	active, _, err := e.documents.Generate(ctx, e.client, request("active"), "")
	if err != nil || *active.TemplateVersionID != v2.ID {
		t.Fatalf("active version: %v, %v; want v2", active.TemplateVersionID, err)
	}

	pinned := request("pinned")
	pinned.Version = 1
	doc, _, err := e.documents.Generate(ctx, e.client, pinned, "")
	if err != nil || *doc.TemplateVersionID != e.v1.ID {
		t.Errorf("pinned v1: %v, %v; want v1", doc.TemplateVersionID, err)
	}
}

func TestGenerateErrors(t *testing.T) {
	e := newTestEnv(t)
	ctx := t.Context()

	tests := []struct {
		name      string
		change    func(r *GenerateRequest)
		wantField string
		wantErrs  int
		wantErr   error
	}{
		{name: "no idempotency key", change: func(r *GenerateRequest) { r.IdempotencyKey = "" }, wantField: "idempotency_key"},
		{name: "long idempotency key", change: func(r *GenerateRequest) { r.IdempotencyKey = strings.Repeat("k", 256) }, wantField: "idempotency_key"},
		{name: "unknown format", change: func(r *GenerateRequest) { r.Format = "odt" }, wantField: "format"},
		{name: "docx from html template", change: func(r *GenerateRequest) { r.Format = "docx" }, wantField: "format"},
		{name: "data does not match schema", change: func(r *GenerateRequest) { r.Data = json.RawMessage(`{"sum": "много"}`) }, wantField: "data", wantErrs: 2},
		{name: "unknown template", change: func(r *GenerateRequest) { r.Template = "nope" }, wantErr: templates.ErrTemplateNotFound},
		{name: "unknown version", change: func(r *GenerateRequest) { r.Version = 9 }, wantErr: templates.ErrVersionNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := request("errors-" + tt.name)
			tt.change(&req)

			_, _, err := e.documents.Generate(ctx, e.client, req, "")

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			var vErr *templates.ValidationError
			if !errors.As(err, &vErr) || vErr.Field != tt.wantField || len(vErr.Errors) != tt.wantErrs {
				t.Errorf("err = %v, want ValidationError on %s with %d field errors", err, tt.wantField, tt.wantErrs)
			}
		})
	}

	var count int
	if err := e.tx.QueryRow(ctx, "SELECT count(*) FROM documents WHERE project = $1", e.client.Project).Scan(&count); err != nil || count != 0 {
		t.Errorf("documents after failed requests = %d, %v; want 0", count, err)
	}
}

// recordingConverter запоминает документ вместо конвертации.
type recordingConverter struct {
	doc gotenberg.Document
}

func (r *recordingConverter) ConvertDOCX(_ context.Context, docx []byte, _ string) ([]byte, error) {
	return append([]byte("%PDF-"), docx[:min(len(docx), 4)]...), nil
}

func (r *recordingConverter) ConvertHTML(_ context.Context, doc gotenberg.Document, _ string) ([]byte, error) {
	r.doc = doc
	return []byte("%PDF-recorded"), nil
}

func TestGenerateFooterHasPublicID(t *testing.T) {
	e := newTestEnv(t)
	rec := &recordingConverter{}
	svc := NewService(e.tx, e.templates, rec, e.files, Buckets{Documents: e.docBucket, Templates: e.docBucket}, testVerifyBase)

	doc, _, err := svc.Generate(t.Context(), e.client, request("footer"), "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !bytes.Contains(rec.doc.Footer, []byte(doc.PublicID)) {
		t.Errorf("footer has no public id %s:\n%s", doc.PublicID, rec.doc.Footer)
	}
	if !bytes.Contains(rec.doc.HTML, []byte(testVerifyBase+doc.PublicID)) || !bytes.Contains(rec.doc.HTML, []byte(`src="data:image/png;base64,`)) {
		t.Errorf("page has no verify link or QR for %s", doc.PublicID)
	}
	if bytes.Contains(rec.doc.HTML, []byte("ОБРАЗЕЦ")) {
		t.Error("real document must not carry the preview watermark")
	}
}
