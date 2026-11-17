package documents

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"docgen/internal/db"
	"docgen/internal/docx"
)

// testDocx — DOCX-шаблон протокола с циклом по лотам.
func testDocx(t *testing.T, body string) []byte {
	t.Helper()
	files := []struct{ name, content string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			body + `<w:sectPr/></w:body></w:document>`},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(f.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func para(text string) string {
	return `<w:p><w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
}

var protocolDocx = para("Протокол № {{ number }}") + para("{{#each lots}}{{@number}}. {{name}} — {{amount | money}}; {{/each}}")

func docxRequest(t *testing.T, key, format string) DocxRequest {
	return DocxRequest{
		Template:       testDocx(t, protocolDocx),
		Data:           []byte(`{"number": "0001", "lots": [{"name": "Бумага", "amount": 6000000}]}`),
		Format:         format,
		TemplateName:   "opening_protocol",
		ExternalID:     "lot:209092",
		RequestedBy:    "user-17",
		RetentionDays:  30,
		IdempotencyKey: key,
	}
}

// documentText — текст основного документа DOCX без разметки.
func documentText(t *testing.T, file []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("not a docx: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			_ = r.Close()
			return regexp.MustCompile(`<[^>]+>`).ReplaceAllString(string(b), "")
		}
	}
	t.Fatal("no word/document.xml")
	return ""
}

func TestGenerateDOCX(t *testing.T) {
	e := newTestEnv(t)
	ctx := t.Context()
	req := docxRequest(t, "docx-1", "docx")

	gen, err := e.documents.GenerateDOCX(ctx, e.client, req, "")
	if err != nil {
		t.Fatalf("GenerateDOCX: %v", err)
	}
	doc := gen.Document
	if !gen.Created || doc.Format != "docx" || *doc.TemplateName != "opening_protocol" || *doc.ExternalID != "lot:209092" ||
		*doc.RequestedBy != "user-17" || doc.TemplateVersionID != nil {
		t.Errorf("doc = %+v", doc)
	}
	if text := documentText(t, gen.File); !strings.Contains(text, "Протокол № 0001") || !strings.Contains(text, "6 000 000.00") {
		t.Errorf("rendered text: %s", text)
	}
	if days := doc.ExpiresAt.Sub(doc.CreatedAt).Hours() / 24; days < 29.9 || days > 30.1 {
		t.Errorf("retention = %.1f days, want 30", days)
	}

	// Копия шаблона сохранена: строка и файл с тем же SHA-256.
	sum := sha256.Sum256(req.Template)
	if !bytes.Equal(doc.TemplateSha256, sum[:]) {
		t.Errorf("template sha256 = %x", doc.TemplateSha256)
	}
	copyRow, err := db.New(e.tx).GetTemplateFile(ctx, sum[:])
	if err != nil {
		t.Fatalf("template copy row: %v", err)
	}
	r, err := e.files.Get(ctx, e.docBucket, copyRow.FileKey)
	if err != nil {
		t.Fatalf("template copy file: %v", err)
	}
	stored, _ := io.ReadAll(r)
	_ = r.Close()
	if !bytes.Equal(stored, req.Template) || copyRow.FileKey != hex.EncodeToString(sum[:])+".docx" {
		t.Errorf("template copy %s differs from the request", copyRow.FileKey)
	}

	// Повтор того же запроса — тот же документ и тот же файл.
	again, err := e.documents.GenerateDOCX(ctx, e.client, req, "")
	if err != nil || again.Created || again.Document.ID != doc.ID || !bytes.Equal(again.File, gen.File) {
		t.Errorf("repeat: created=%v id=%v same file=%v err=%v", again.Created, again.Document.ID, bytes.Equal(again.File, gen.File), err)
	}

	// Тот же ключ, другие данные — конфликт.
	other := req
	other.Data = []byte(`{"number": "0002", "lots": []}`)
	if _, err := e.documents.GenerateDOCX(ctx, e.client, other, ""); !errors.Is(err, ErrIdempotencyConflict) {
		t.Errorf("other data: err = %v, want ErrIdempotencyConflict", err)
	}
	// Тот же ключ, другой шаблон — тоже конфликт: шаблон входит в отпечаток запроса.
	other = req
	other.Template = testDocx(t, para("Другой {{ number }}"))
	if _, err := e.documents.GenerateDOCX(ctx, e.client, other, ""); !errors.Is(err, ErrIdempotencyConflict) {
		t.Errorf("other template: err = %v, want ErrIdempotencyConflict", err)
	}

	// Метаданные для портала.
	view, err := e.documents.Get(ctx, e.client.Project, doc.ID)
	if err != nil || view.DisplayTemplateName != "opening_protocol" {
		t.Errorf("Get = %+v, %v", view, err)
	}
}

func TestGenerateDOCXToPDF(t *testing.T) {
	e := newTestEnv(t)

	gen, err := e.documents.GenerateDOCX(t.Context(), e.client, docxRequest(t, "pdf-1", "pdf"), "")
	if err != nil {
		t.Fatalf("GenerateDOCX: %v", err)
	}
	if !bytes.HasPrefix(gen.File, []byte("%PDF-")) || gen.Document.Format != "pdf" || gen.Document.FileSize != int64(len(gen.File)) {
		t.Errorf("pdf: %d bytes, format %s", len(gen.File), gen.Document.Format)
	}
	if !strings.HasSuffix(gen.Document.FileKey, ".pdf") {
		t.Errorf("file key = %s", gen.Document.FileKey)
	}
}

func TestGenerateDOCXErrors(t *testing.T) {
	e := newTestEnv(t)
	ctx := t.Context()

	tests := []struct {
		name   string
		change func(r *DocxRequest)
		check  func(err error) bool
	}{
		{"retention above project", func(r *DocxRequest) { r.RetentionDays = 366 }, isField("retention_days")},
		{"no template name", func(r *DocxRequest) { r.TemplateName = "" }, isField("template_name")},
		{"no external id", func(r *DocxRequest) { r.ExternalID = "" }, isField("external_id")},
		{"no requested by", func(r *DocxRequest) { r.RequestedBy = "" }, isField("requested_by")},
		{"bad format", func(r *DocxRequest) { r.Format = "odt" }, isField("format")},
		{"data not object", func(r *DocxRequest) { r.Data = []byte(`[1,2]`) }, isField("data")},
		{"broken template", func(r *DocxRequest) { r.Template = testDocx(t, para("{{#each lots}}")) }, func(err error) bool {
			var tErr *docx.TemplateError
			return errors.As(err, &tErr)
		}},
		{"missing data", func(r *DocxRequest) { r.Data = []byte(`{"lots": [{"name": "x"}]}`) }, func(err error) bool {
			var dErr *docx.DataError
			return errors.As(err, &dErr) && len(dErr.Fields) == 2
		}},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := docxRequest(t, fmt.Sprintf("err-%d", i), "docx")
			tt.change(&req)
			if _, err := e.documents.GenerateDOCX(ctx, e.client, req, ""); !tt.check(err) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func isField(field string) func(error) bool {
	return func(err error) bool {
		var vErr *ValidationError
		return errors.As(err, &vErr) && vErr.Field == field
	}
}

func TestCleanupRemovesOrphanTemplateCopies(t *testing.T) {
	e := newTestEnv(t)
	ctx := t.Context()

	gen, err := e.documents.GenerateDOCX(ctx, e.client, docxRequest(t, "cleanup-1", "docx"), "")
	if err != nil {
		t.Fatal(err)
	}
	sum := gen.Document.TemplateSha256

	// Пока документ жив, копия шаблона нужна.
	if _, err := e.documents.DeleteOrphanTemplates(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := db.New(e.tx).GetTemplateFile(ctx, sum); err != nil {
		t.Fatalf("template copy deleted while its document exists: %v", err)
	}

	later := *e.documents
	later.now = func() time.Time { return gen.Document.ExpiresAt }
	if _, err := later.DeleteExpired(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := later.DeleteOrphanTemplates(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := db.New(e.tx).GetTemplateFile(ctx, sum); err == nil {
		t.Error("template copy still exists after its last document expired")
	}
	if _, err := e.files.Get(ctx, e.docBucket, hex.EncodeToString(sum)+".docx"); err == nil {
		t.Error("template copy file still exists")
	}
}
