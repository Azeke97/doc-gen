package templates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"docgen/internal/gotenberg"
	"docgen/internal/htmlengine"
)

func TestPreview(t *testing.T) {
	svc, _, project := newTestService(t)
	ctx := t.Context()

	tmpl, err := svc.Create(ctx, "alice", validParams(project))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	page := []byte(`<!doctype html><html><head><meta charset="utf-8"><style>@page { size: A4 landscape; }</style></head><body><h1>{{.title}}</h1></body></html>`)
	version, _, err := svc.UploadVersion(ctx, "alice", tmpl.ID, UploadParams{
		File: page, Schema: testSchema, TestData: json.RawMessage(`{"title":"Хаттама / Протокол"}`),
	})
	if err != nil {
		t.Fatalf("UploadVersion: %v", err)
	}

	pdf, err := svc.Preview(ctx, version.ID, "preview-test")
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Errorf("result is not a PDF: %q", pdf[:min(len(pdf), 20)])
	}

	// Что именно уходит в Gotenberg: водяной знак в странице и ID версии в колонтитуле.
	rec := &recordingConverter{}
	if _, err := NewService(svc.pool, svc.files, svc.bucket, rec, testVerifyBase).Preview(ctx, version.ID, ""); err != nil {
		t.Fatalf("Preview with recorder: %v", err)
	}
	if !bytes.Contains(rec.doc.HTML, []byte(htmlengine.PreviewWatermark)) {
		t.Error("page has no preview watermark")
	}
	if !bytes.Contains(rec.doc.Footer, []byte(strings.ReplaceAll(version.ID.String(), "-", ""))) {
		t.Errorf("footer has no version id:\n%s", rec.doc.Footer)
	}

	// Файл в хранилище подменили — предпросмотр отказывается его использовать.
	if err := svc.files.Put(ctx, svc.bucket, version.FileKey, bytes.NewReader([]byte("<h1>forged</h1>")), 15, "text/html"); err != nil {
		t.Fatalf("forge file: %v", err)
	}
	if _, err := svc.Preview(ctx, version.ID, ""); err == nil {
		t.Error("forged file: want error")
	}
}

// recordingConverter запоминает документ вместо конвертации.
type recordingConverter struct {
	doc gotenberg.Document
}

func (r *recordingConverter) ConvertHTML(_ context.Context, doc gotenberg.Document, _ string) ([]byte, error) {
	r.doc = doc
	return []byte("%PDF-"), nil
}

func TestPreviewErrors(t *testing.T) {
	svc, _, project := newTestService(t)
	ctx := t.Context()

	_, noData := uploadDraft(t, svc, project, uuid.Nil)
	if _, err := svc.Preview(ctx, noData.ID, ""); !errors.Is(err, ErrNoTestData) {
		t.Errorf("no test data: err = %v, want ErrNoTestData", err)
	}

	if _, err := svc.Preview(ctx, uuid.New(), ""); !errors.Is(err, ErrVersionNotFound) {
		t.Errorf("unknown version: err = %v, want ErrVersionNotFound", err)
	}

	p := validParams(project)
	p.Code, p.Engine = "contract", "docx"
	docx, err := svc.Create(ctx, "alice", p)
	if err != nil {
		t.Fatalf("Create docx: %v", err)
	}
	v, _, err := svc.UploadVersion(ctx, "alice", docx.ID, UploadParams{File: []byte("PK\x03\x04docx"), Schema: testSchema})
	if err != nil {
		t.Fatalf("Upload docx: %v", err)
	}
	if _, err := svc.Preview(ctx, v.ID, ""); !errors.Is(err, ErrPreviewNotSupported) {
		t.Errorf("docx: err = %v, want ErrPreviewNotSupported", err)
	}
}
