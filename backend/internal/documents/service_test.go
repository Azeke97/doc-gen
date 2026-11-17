package documents

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"docgen/internal/db"
	"docgen/internal/gotenberg"
	"docgen/internal/storage"
)

// testDocumentsBucket — отдельный бакет для тестов: и документы, и копии шаблонов. Очистка
// ищет просроченные документы по всей базе, а тесты работают с базой разработчика: строки
// вернёт откат транзакции, а файлы из общего бакета удалились бы по-настоящему.
const testDocumentsBucket = "docgen-test-documents"

// env — настоящие Postgres (в откатываемой транзакции), MinIO и Gotenberg.
type env struct {
	tx        pgx.Tx
	files     *storage.Store
	docBucket string
	documents *Service
	client    db.APIClient
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
	if err := files.EnsureBucket(ctx, testDocumentsBucket); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	project := fmt.Sprintf("test_%d", time.Now().UnixNano())

	// Хранилище транзакция не откатит: удаляем файлы этого проекта сами, потом откатываем базу.
	t.Cleanup(func() {
		ctx := context.Background()
		removeFiles(ctx, tx, files, "SELECT file_key FROM documents WHERE project = $1", project)
		removeFiles(ctx, tx, files,
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
	return &env{
		tx:        tx,
		files:     files,
		docBucket: testDocumentsBucket,
		documents: NewService(tx, pdf, files, Buckets{Documents: testDocumentsBucket, Templates: testDocumentsBucket}),
		client:    client,
	}
}

func removeFiles(ctx context.Context, tx pgx.Tx, files *storage.Store, query, project string) {
	rows, err := tx.Query(ctx, query, project)
	if err != nil {
		return
	}
	keys, _ := pgx.CollectRows(rows, pgx.RowTo[string])
	for _, key := range keys {
		_ = files.Delete(ctx, testDocumentsBucket, key)
	}
}
