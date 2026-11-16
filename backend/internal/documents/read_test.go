package documents

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGetAndFile(t *testing.T) {
	e := newTestEnv(t)
	ctx := t.Context()

	created, _, err := e.documents.Generate(ctx, e.client, request("read"), "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	doc, err := e.documents.Get(ctx, e.client.Project, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if doc.TemplateCode != "opening_protocol" || doc.Locale != "ru" || doc.Version != 1 || doc.Document.ID != created.ID {
		t.Errorf("doc = %+v", doc)
	}

	_, pdf, err := e.documents.File(ctx, e.client.Project, created.ID)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) || int64(len(pdf)) != created.FileSize {
		t.Errorf("File = %d bytes, %v", len(pdf), err)
	}

	// Чужой проект и несуществующий id — одинаково «не найден».
	if _, err := e.documents.Get(ctx, "other_project", created.ID); !errors.Is(err, ErrDocumentNotFound) {
		t.Errorf("other project: err = %v, want ErrDocumentNotFound", err)
	}
	if _, err := e.documents.Get(ctx, e.client.Project, uuid.New()); !errors.Is(err, ErrDocumentNotFound) {
		t.Errorf("unknown id: err = %v, want ErrDocumentNotFound", err)
	}

	// Срок хранения истёк — документа для портала больше нет.
	later := *e.documents
	later.now = func() time.Time { return created.ExpiresAt }
	if _, err := later.Get(ctx, e.client.Project, created.ID); !errors.Is(err, ErrDocumentExpired) {
		t.Errorf("expired: err = %v, want ErrDocumentExpired", err)
	}

	// Файл в хранилище подменили — отдавать его нельзя.
	forged := []byte("%PDF-forged")
	if err := e.files.Put(ctx, os.Getenv("S3_DOCUMENTS_BUCKET"), created.FileKey, bytes.NewReader(forged), int64(len(forged)), "application/pdf"); err != nil {
		t.Fatalf("forge file: %v", err)
	}
	if _, _, err := e.documents.File(ctx, e.client.Project, created.ID); err == nil {
		t.Error("forged file: want error")
	}
}
