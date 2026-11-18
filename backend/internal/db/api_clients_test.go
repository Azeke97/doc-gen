package db_test

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"docgen/internal/db"
	"docgen/internal/pgerr"
)

// TestDeleteAPIClient: удаляется только отозванный ключ без журнала и документов.
func TestDeleteAPIClient(t *testing.T) {
	if os.Getenv("DOCGEN_INTEGRATION") != "1" {
		t.Skip("set DOCGEN_INTEGRATION=1 to run integration tests")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, os.Getenv("DB_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	q := db.New(tx)

	project := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := q.CreateProject(ctx, db.CreateProjectParams{Code: project, Name: "Тест"}); err != nil {
		t.Fatal(err)
	}
	newKey := func(name string) db.APIClient {
		t.Helper()
		k, err := q.CreateAPIClient(ctx, db.CreateAPIClientParams{Name: name, Project: project, KeyHash: []byte(project + name), KeyPrefix: "dg_t"})
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	unused, used := newKey("unused"), newKey("used")
	if _, err := tx.Exec(ctx, `INSERT INTO generation_log (api_client_id, project, request_id, template_name, status, http_status, error_code, duration_ms)
		VALUES ($1, $2, 'r1', 't', 'rejected', 400, 'invalid_data', 1)`, used.ID, project); err != nil {
		t.Fatal(err)
	}

	if _, err := q.DeleteAPIClient(ctx, unused.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("delete active key: err = %v, want ErrNoRows", err)
	}
	for _, k := range []db.APIClient{unused, used} {
		if _, err := q.RevokeAPIClient(ctx, k.ID); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := q.ListAPIClients(ctx, &project)
	if err != nil {
		t.Fatal(err)
	}
	usedByName := map[string]bool{}
	for _, r := range rows {
		usedByName[r.Name] = r.Used
	}
	if usedByName["unused"] || !usedByName["used"] {
		t.Errorf("used flags = %v", usedByName)
	}

	// Ошибка внешнего ключа обрывает транзакцию, поэтому пробуем в точке сохранения.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.New(sp).DeleteAPIClient(ctx, used.ID); !pgerr.IsForeignKeyViolation(err) {
		t.Errorf("delete used key: err = %v, want FK violation", err)
	}
	_ = sp.Rollback(ctx)

	if k, err := q.DeleteAPIClient(ctx, unused.ID); err != nil || k.ID != unused.ID {
		t.Errorf("delete unused key: %+v, %v", k, err)
	}
}
