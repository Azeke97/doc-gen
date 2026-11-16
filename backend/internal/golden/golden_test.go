// Package golden — эталонные тесты документов (ТЗ, детерминизм): одна версия шаблона
// и одни данные дают тот же HTML и тот же PDF. Эталоны лежат в testdata; после
// осознанного изменения шаблона, хелперов или Gotenberg их обновляют:
//
//	DOCGEN_INTEGRATION=1 go test ./internal/golden -update
package golden

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"regexp"
	"testing"
	"time"

	"docgen/internal/dataschema"
	"docgen/internal/gotenberg"
	"docgen/internal/htmlengine"
)

var update = flag.Bool("update", false, "rewrite golden files")

// Фиксированные значения документа: случайный public_id сделал бы результат разным.
var options = htmlengine.RenderOptions{
	DocumentID: "0195f3c9a1e54b7d8c2a8e6f1b3d5a7c",
	VerifyURL:  "https://docgen.example.kz/v1/verify/0195f3c9a1e54b7d8c2a8e6f1b3d5a7c",
}

// testdata открывает папку с эталонами: os.Root не даёт выйти за её пределы.
func testdata(t *testing.T) *os.Root {
	t.Helper()

	root, err := os.OpenRoot("testdata")
	if err != nil {
		t.Fatalf("open testdata: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func read(t *testing.T, name string) []byte {
	t.Helper()

	b, err := testdata(t).ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

// compareGolden сравнивает результат с эталоном или, с -update, перезаписывает эталон.
func compareGolden(t *testing.T, name string, got []byte) {
	t.Helper()

	root := testdata(t)
	if *update {
		if err := root.WriteFile(name, got, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return
	}

	want, err := root.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s (run with -update to create it): %v", name, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden file (%d vs %d bytes); if the change is intended, run with -update", name, len(got), len(want))
	}
}

// renderProtocol проверяет данные по схеме и отрисовывает пилотный протокол.
func renderProtocol(t *testing.T) []byte {
	t.Helper()

	schema, err := dataschema.Compile(read(t, "opening_protocol.schema.json"))
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	data := json.RawMessage(read(t, "opening_protocol.data.json"))
	if fieldErrs, err := schema.Validate(data); err != nil || len(fieldErrs) > 0 {
		t.Fatalf("test data does not match the schema: %v %+v", err, fieldErrs)
	}

	tmpl, err := htmlengine.Parse(read(t, "opening_protocol.html"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	html, err := tmpl.Render(data, options)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return html
}

func TestOpeningProtocolHTML(t *testing.T) {
	first := renderProtocol(t)
	if second := renderProtocol(t); !bytes.Equal(first, second) {
		t.Fatal("two renders of the same data differ")
	}

	compareGolden(t, "opening_protocol.golden.html", first)
}

// pdfDates — даты создания, единственное, чем отличаются PDF одного документа.
var pdfDates = regexp.MustCompile(`/(CreationDate|ModDate) \(D:[^)]*\)`)

var pdfPage = regexp.MustCompile(`/Type\s*/Page[^s]`)

func TestOpeningProtocolPDF(t *testing.T) {
	if os.Getenv("DOCGEN_INTEGRATION") != "1" {
		t.Skip("set DOCGEN_INTEGRATION=1 to run integration tests")
	}

	pdf, err := gotenberg.New(os.Getenv("GOTENBERG_URL"), 30*time.Second).ConvertHTML(t.Context(), gotenberg.Document{
		HTML:   renderProtocol(t),
		Footer: htmlengine.Footer(options.DocumentID),
	}, "golden")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	// Казахский и русский блоки начинаются с новой страницы — страниц минимум две.
	if pages := len(pdfPage.FindAll(pdf, -1)); pages < 2 {
		t.Errorf("pages = %d, want at least 2", pages)
	}

	compareGolden(t, "opening_protocol.golden.pdf", pdfDates.ReplaceAll(pdf, nil))
}
