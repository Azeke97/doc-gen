package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

// newTestStore подключается к настоящему S3 из переменных S3_* и создаёт отдельный бакет на время теста.
// Тест запускается только с DOCGEN_INTEGRATION=1, чтобы `make test` не зависел от поднятого MinIO.
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()

	if os.Getenv("DOCGEN_INTEGRATION") != "1" {
		t.Skip("set DOCGEN_INTEGRATION=1 to run integration tests")
	}

	store, err := New(Config{
		Endpoint:  os.Getenv("S3_ENDPOINT"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		UseSSL:    os.Getenv("S3_USE_SSL") == "true",
		Region:    os.Getenv("S3_REGION"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	bucket := fmt.Sprintf("docgen-test-%d", time.Now().UnixNano())
	if err := store.EnsureBucket(t.Context(), bucket); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		for obj := range store.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
			_ = store.client.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{})
		}
		if err := store.client.RemoveBucket(ctx, bucket); err != nil {
			t.Errorf("remove test bucket: %v", err)
		}
	})

	return store, bucket
}

func TestPutGetDelete(t *testing.T) {
	store, bucket := newTestStore(t)
	ctx := t.Context()
	content := []byte("hello, docgen")

	if err := store.Put(ctx, bucket, "dir/file.txt", bytes.NewReader(content), int64(len(content)), "text/plain"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	rc, err := store.Get(ctx, bucket, "dir/file.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content = %q, want %q", got, content)
	}

	if err := store.Delete(ctx, bucket, "dir/file.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, bucket, "dir/file.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestGetMissing(t *testing.T) {
	store, bucket := newTestStore(t)

	if _, err := store.Get(t.Context(), bucket, "no/such/key"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestEnsureBucketIsIdempotent(t *testing.T) {
	store, bucket := newTestStore(t)

	if err := store.EnsureBucket(t.Context(), bucket); err != nil {
		t.Errorf("second EnsureBucket: %v", err)
	}
}
