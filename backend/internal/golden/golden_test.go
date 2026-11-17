// Package golden — эталонные тесты: синтетический двуязычный протокол вскрытия
// (testdata/opening_protocol.docx и .json) превращается в DOCX и PDF и сравнивается
// с эталонами testdata/*.golden.*. После намеренного изменения движка, форматтеров,
// шаблона или Gotenberg эталоны обновляют:
//
//	DOCGEN_INTEGRATION=1 go test ./internal/golden -update
//
// затем открывают golden-файлы глазами и коммитят.
package golden

import (
	"bytes"
	"flag"
	"os"
	"regexp"
	"testing"
	"time"

	"docgen/internal/docx"
	"docgen/internal/gotenberg"
)

var update = flag.Bool("update", false, "перезаписать эталоны")

const (
	templateFile   = "testdata/opening_protocol.docx"
	dataFile       = "testdata/opening_protocol.json"
	goldenDOCXFile = "testdata/opening_protocol.golden.docx"
	goldenPDFFile  = "testdata/opening_protocol.golden.pdf"
)

// renderProtocol подставляет данные в шаблон протокола.
func renderProtocol(t *testing.T) []byte {
	t.Helper()
	file, err := os.ReadFile(templateFile)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dataFile)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := docx.Parse(file)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	data, err := docx.DecodeData(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := tmpl.Render(data)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return out
}

// DOCX детерминирован: тот же шаблон и те же данные дают те же байты.
func TestGoldenDOCX(t *testing.T) {
	out := renderProtocol(t)
	if *update {
		if err := os.WriteFile(goldenDOCXFile, out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenDOCXFile)
	if err != nil {
		t.Fatalf("no golden file (run with -update): %v", err)
	}
	if !bytes.Equal(out, want) {
		t.Errorf("rendered DOCX differs from %s; if the change is intended, run with -update and review the file", goldenDOCXFile)
	}
}

// Метаданные PDF, которые LibreOffice меняет при каждой конвертации: даты создания (и в XMP),
// ID файла и его контрольная сумма. Остальное совпадает байт в байт.
var volatilePDF = []*regexp.Regexp{
	regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`),
	regexp.MustCompile(`D:\d{14}(Z|[+-]\d{2}'\d{2}')?'?`),
	regexp.MustCompile(`/ID \[ ?<[0-9A-Fa-f]+>\s*<[0-9A-Fa-f]+> ?\]`),
	regexp.MustCompile(`/DocChecksum /[0-9A-Fa-f]+`),
}

func normalizePDF(pdf []byte) []byte {
	for _, re := range volatilePDF {
		pdf = re.ReplaceAll(pdf, []byte("<volatile>"))
	}
	return pdf
}

// PDF через настоящий Gotenberg (make up): ловит изменения шрифтов, вёрстки LibreOffice
// и самого Gotenberg.
func TestGoldenPDF(t *testing.T) {
	if os.Getenv("DOCGEN_INTEGRATION") != "1" {
		t.Skip("set DOCGEN_INTEGRATION=1 to run integration tests")
	}
	c := gotenberg.New(os.Getenv("GOTENBERG_URL"), 60*time.Second)
	pdf, err := c.ConvertDOCX(t.Context(), renderProtocol(t), "golden-test")
	if err != nil {
		t.Fatalf("ConvertDOCX: %v", err)
	}
	if *update {
		if err := os.WriteFile(goldenPDFFile, pdf, 0o600); err != nil { //nolint:gosec // путь — константа теста, а не ввод
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPDFFile)
	if err != nil {
		t.Fatalf("no golden file (run with -update): %v", err)
	}
	if !bytes.Equal(normalizePDF(pdf), normalizePDF(want)) {
		_ = os.WriteFile(goldenPDFFile+".actual", pdf, 0o600) //nolint:gosec // путь — константа теста, а не ввод
		t.Errorf("PDF differs from %s (saved the new one as .actual); if intended, run with -update and review", goldenPDFFile)
	}
}
