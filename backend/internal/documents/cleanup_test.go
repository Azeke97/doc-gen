package documents

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"docgen/internal/storage"
)

func TestDeleteExpired(t *testing.T) {
	e := newTestEnv(t)
	ctx := t.Context()

	for i := range 3 {
		if _, err := e.documents.GenerateDOCX(ctx, e.client, docxRequest(t, fmt.Sprintf("cleanup-%d", i), "docx"), ""); err != nil {
			t.Fatalf("GenerateDOCX %d: %v", i, err)
		}
	}
	var keys []string
	rows, err := e.tx.Query(ctx, "SELECT file_key FROM documents WHERE project = $1", e.client.Project)
	if err != nil {
		t.Fatalf("query keys: %v", err)
	}
	for rows.Next() {
		var k string
		_ = rows.Scan(&k)
		keys = append(keys, k)
	}

	// Сейчас срок хранения (30 дней) ни у кого не истёк.
	if n, err := e.documents.DeleteExpired(ctx, 10); err != nil || n != 0 {
		t.Fatalf("DeleteExpired now = %d, %v; want 0", n, err)
	}

	// Через 31 день — истёк у всех. Пачками по 2: сначала 2, потом остальное.
	later := *e.documents
	later.now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }

	if n, err := later.DeleteExpired(ctx, 2); err != nil || n != 2 {
		t.Fatalf("first batch = %d, %v; want 2", n, err)
	}
	later.cleanup(ctx, 2)

	var left int
	if err := e.tx.QueryRow(ctx, "SELECT count(*) FROM documents WHERE project = $1", e.client.Project).Scan(&left); err != nil || left != 0 {
		t.Errorf("documents left = %d, %v; want 0", left, err)
	}
	for _, k := range keys {
		if _, err := e.files.Get(ctx, e.docBucket, k); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("file %s: err = %v, want ErrNotFound", k, err)
		}
	}
}
