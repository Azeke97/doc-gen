package templates

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
)

var (
	testSchema = []byte(`{"type":"object","properties":{"title":{"type":"string"}}}`)
	testHTML   = []byte(`<h1>{{.title}}</h1>`)
)

func TestUploadVersionLifecycle(t *testing.T) {
	svc, tx, project := newTestService(t)
	ctx := t.Context()

	tmpl, err := svc.Create(ctx, "alice", validParams(project))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	v1, created, err := svc.UploadVersion(ctx, "alice", tmpl.ID, UploadParams{File: testHTML, Schema: testSchema})
	if err != nil {
		t.Fatalf("first upload: %v", err)
	}
	if !created || v1.Version != 1 || v1.Status != "draft" {
		t.Errorf("first upload = created %v, version %d, status %s; want true, 1, draft", created, v1.Version, v1.Status)
	}
	if v1.TestData != nil {
		t.Errorf("test_data = %s, want NULL", v1.TestData)
	}

	// Файл лежит в хранилище, и это ровно то, что загрузили.
	rc, err := svc.files.Get(ctx, svc.bucket, v1.FileKey)
	if err != nil {
		t.Fatalf("get file: %v", err)
	}
	stored, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(stored, testHTML) {
		t.Errorf("stored file = %q, want %q", stored, testHTML)
	}

	// Повторная загрузка заменяет черновик, а не создаёт вторую версию.
	newHTML := []byte(`<h1>{{.title}}!</h1>`)
	v1b, created, err := svc.UploadVersion(ctx, "bob", tmpl.ID, UploadParams{File: newHTML, Schema: testSchema, TestData: []byte(`{"title":"x"}`)})
	if err != nil {
		t.Fatalf("second upload: %v", err)
	}
	if created || v1b.ID != v1.ID || v1b.Version != 1 {
		t.Errorf("second upload = created %v, id same %v, version %d; want false, true, 1", created, v1b.ID == v1.ID, v1b.Version)
	}
	if hex.EncodeToString(v1b.FileSha256) == hex.EncodeToString(v1.FileSha256) {
		t.Error("sha256 did not change after replacing the file")
	}
	if v1b.CreatedBy != "bob" {
		t.Errorf("created_by = %q, want bob", v1b.CreatedBy)
	}

	var audits int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM audit_log WHERE entity_id = $1 AND action = $2",
		v1.ID, ActionVersionUploaded).Scan(&audits); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if audits != 2 {
		t.Errorf("upload audit entries = %d, want 2", audits)
	}
}

func TestUploadVersionErrors(t *testing.T) {
	svc, tx, project := newTestService(t)
	ctx := t.Context()

	tmpl, err := svc.Create(ctx, "alice", validParams(project))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, _, err = svc.UploadVersion(ctx, "alice", uuid.New(), UploadParams{File: testHTML, Schema: testSchema})
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("unknown template: err = %v, want ErrTemplateNotFound", err)
	}

	_, _, err = svc.UploadVersion(ctx, "alice", tmpl.ID, UploadParams{File: []byte(`<h1>{{.title</h1>`), Schema: testSchema})
	var vErr *ValidationError
	if !errors.As(err, &vErr) || vErr.Field != "file" {
		t.Errorf("broken template: err = %v, want ValidationError on file", err)
	}

	if _, err := tx.Exec(ctx, "UPDATE templates SET status = 'archived' WHERE id = $1", tmpl.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	_, _, err = svc.UploadVersion(ctx, "alice", tmpl.ID, UploadParams{File: testHTML, Schema: testSchema})
	if !errors.Is(err, ErrTemplateArchived) {
		t.Errorf("archived: err = %v, want ErrTemplateArchived", err)
	}
}

func TestValidateUpload(t *testing.T) {
	tests := []struct {
		name      string
		engine    string
		p         UploadParams
		wantField string
	}{
		{name: "ok html", engine: "html", p: UploadParams{File: testHTML, Schema: testSchema}},
		{name: "empty file", engine: "html", p: UploadParams{Schema: testSchema}, wantField: "file"},
		{name: "too big", engine: "html", p: UploadParams{File: make([]byte, MaxFileSize+1), Schema: testSchema}, wantField: "file"},
		{name: "schema not object", engine: "html", p: UploadParams{File: testHTML, Schema: []byte(`[1,2]`)}, wantField: "schema"},
		{name: "schema missing", engine: "html", p: UploadParams{File: testHTML}, wantField: "schema"},
		{name: "bad test data", engine: "html", p: UploadParams{File: testHTML, Schema: testSchema, TestData: []byte(`{`)}, wantField: "test_data"},
		{name: "broken html template", engine: "html", p: UploadParams{File: []byte(`{{if}}`), Schema: testSchema}, wantField: "file"},
		{name: "docx not zip", engine: "docx", p: UploadParams{File: testHTML, Schema: testSchema}, wantField: "file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUpload(tt.engine, tt.p)

			if tt.wantField == "" {
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
				return
			}

			var vErr *ValidationError
			if !errors.As(err, &vErr) || vErr.Field != tt.wantField {
				t.Errorf("err = %v, want ValidationError on %q", err, tt.wantField)
			}
		})
	}
}
