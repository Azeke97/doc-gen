package docx

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/beevik/etree"
)

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/word/document.xml" ContentType="%s"/></Types>`

// makeDocx собирает минимальный DOCX: body — содержимое w:body, extra — другие части архива.
func makeDocx(t *testing.T, body string, extra map[string]string) []byte {
	t.Helper()
	files := map[string]string{
		"[Content_Types].xml": strings.Replace(contentTypes, "%s", mainContentType, 1),
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<w:document xmlns:w="` + wordNS + `" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" mc:Ignorable=""><w:body>` +
			body + `<w:sectPr/></w:body></w:document>`,
	}
	for k, v := range extra {
		files[k] = v
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml"} {
		writeFile(t, zw, name, files[name])
		delete(files, name)
	}
	for name, content := range files {
		writeFile(t, zw, name, content)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeFile(t *testing.T, zw *zip.Writer, name, content string) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
}

// p — абзац из прогонов: каждая строка — отдельный прогон, как Word режет текст.
func p(runs ...string) string {
	var b strings.Builder
	b.WriteString("<w:p>")
	for _, r := range runs {
		b.WriteString(`<w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">` + escape(r) + `</w:t></w:r>`)
	}
	b.WriteString("</w:p>")
	return b.String()
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// row — строка таблицы, по одному абзацу в ячейке.
func row(cells ...string) string {
	var b strings.Builder
	b.WriteString("<w:tr>")
	for _, c := range cells {
		b.WriteString("<w:tc>" + p(c) + "</w:tc>")
	}
	b.WriteString("</w:tr>")
	return b.String()
}

func table(rows ...string) string {
	return "<w:tbl>" + strings.Join(rows, "") + "</w:tbl>"
}

// render разбирает шаблон, подставляет JSON и возвращает текст результата: абзацы через
// перевод строки, ячейки строки таблицы через « | ».
func render(t *testing.T, file []byte, data string) string {
	t.Helper()
	tmpl, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	d, err := DecodeData([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	out, err := tmpl.Render(d)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return documentText(t, out, "word/document.xml")
}

func documentText(t *testing.T, file []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("result is not a zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		b, err := readLimited(f)
		if err != nil {
			t.Fatal(err)
		}
		doc := etree.NewDocument()
		if err := doc.ReadFromBytes(b); err != nil {
			t.Fatalf("result XML: %v", err)
		}
		var lines []string
		var walk func(e *etree.Element)
		walk = func(e *etree.Element) {
			switch e.Tag {
			case "p":
				lines = append(lines, paraText(e))
				return
			case "tr":
				var cells []string
				for _, tc := range e.SelectElements("tc") {
					var ps []string
					for _, pe := range tc.SelectElements("p") {
						ps = append(ps, paraText(pe))
					}
					cells = append(cells, strings.Join(ps, "/"))
				}
				lines = append(lines, strings.Join(cells, " | "))
				return
			}
			for _, c := range e.ChildElements() {
				walk(c)
			}
		}
		walk(doc.Root())
		return strings.Join(lines, "\n")
	}
	t.Fatalf("no %s in result", name)
	return ""
}

func paraText(pe *etree.Element) string {
	var b strings.Builder
	var walk func(e *etree.Element)
	walk = func(e *etree.Element) {
		for _, c := range e.ChildElements() {
			switch c.Tag {
			case "t":
				b.WriteString(c.Text())
			case "br":
				b.WriteString("⏎")
			case "tab":
				b.WriteString("→")
			default:
				walk(c)
			}
		}
	}
	walk(pe)
	return b.String()
}

func TestValuesAndSplitTags(t *testing.T) {
	file := makeDocx(t, p("Протокол № ", "{", "{ num", "ber }", "} от {{ date | date }}")+
		p("Заказчик: {{customer.name}}, БИН {{ customer.bin | bin }}")+
		p("Сумма: {{ total | money }} ({{ total | moneywords:ru }})")+
		p("Примечание: {{ note | default:нет }}; {{ empty | default:— }}")+
		p("Адрес: {{ address }}"), nil)

	got := render(t, file, `{"number":"0001","date":"2026-10-20","customer":{"name":"ТОО «Альфа»","bin":"000740001307"},
		"total":6000000,"empty":"","address":"г. Астана,\nул. Мира 1"}`)

	want := strings.Join([]string{
		"Протокол № 0001 от 20.10.2026",
		"Заказчик: ТОО «Альфа», БИН 000740001307",
		"Сумма: 6 000 000.00 (шесть миллионов тенге 00 тиын)",
		"Примечание: нет; —",
		"Адрес: г. Астана,⏎ул. Мира 1",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormattingKept(t *testing.T) {
	file := makeDocx(t, p("Итого: {{ total | money }}"), nil)
	tmpl, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	out, err := tmpl.Render(map[string]any{"total": "10"})
	if err != nil {
		t.Fatal(err)
	}
	// У прогона со значением осталось форматирование метки (жирный).
	xml := string(partBytes(t, out, "word/document.xml"))
	if !strings.Contains(xml, `<w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">10.00</w:t></w:r>`) {
		t.Errorf("value run lost its formatting:\n%s", xml)
	}
}

func partBytes(t *testing.T, file []byte, name string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			b, err := readLimited(f)
			if err != nil {
				t.Fatal(err)
			}
			return b
		}
	}
	t.Fatalf("no %s", name)
	return nil
}

func TestInlineBlocks(t *testing.T) {
	file := makeDocx(t,
		p("Лоты: {{#each lots}}{{@number}}) {{name}}; {{else}}нет{{/each}}")+
			p("{{#if security}}Обеспечение внесено{{else}}Без обеспечения{{/if}}."), nil)

	got := render(t, file, `{"lots":[{"name":"Бумага"},{"name":"Ручки"}],"security":false}`)
	want := "Лоты: 1) Бумага; 2) Ручки; \nБез обеспечения."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	got = render(t, file, `{"lots":[],"security":true}`)
	want = "Лоты: нет\nОбеспечение внесено."
	if got != want {
		t.Errorf("empty: got %q, want %q", got, want)
	}
}

func TestParagraphBlocks(t *testing.T) {
	file := makeDocx(t,
		p("Члены комиссии:")+
			p("{{#each members}}")+
			p("{{@number}}. {{name}}")+
			p("{{#if chairman}}")+
			p("Председатель комиссии"+" лота {{../lot}}")+
			p("{{/if}}")+
			p("{{else}}")+
			p("Комиссия не назначена")+
			p("{{/each}}")+
			p("Конец."), nil)

	got := render(t, file, `{"lot":"209092","members":[{"name":"Иванов","chairman":true},{"name":"Петров","chairman":false}]}`)
	want := "Члены комиссии:\n1. Иванов\nПредседатель комиссии лота 209092\n2. Петров\nКонец."
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	got = render(t, file, `{"lot":"1","members":[]}`)
	want = "Члены комиссии:\nКомиссия не назначена\nКонец."
	if got != want {
		t.Errorf("empty: got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTableRows(t *testing.T) {
	file := makeDocx(t, table(
		row("№", "Наименование", "Сумма"),
		row("{{#each lots}}{{@number}}", "{{name}}{{#if note}} ({{note}}){{/if}}", "{{amount | money}}"),
		row("{{else}}Нет лотов", "", "{{/each}}"),
		row("", "Итого", "{{ total | money }}"),
	), nil)

	got := render(t, file, `{"lots":[{"name":"Бумага","amount":"100.5","note":"А4"},{"name":"Ручки","amount":20,"note":null}],"total":"120.5"}`)
	want := strings.Join([]string{
		"№ | Наименование | Сумма",
		"1 | Бумага (А4) | 100.50",
		"2 | Ручки | 20.00",
		" | Итого | 120.50",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	got = render(t, file, `{"lots":[],"total":0}`)
	want = "№ | Наименование | Сумма\nНет лотов |  | \n | Итого | 0.00"
	if got != want {
		t.Errorf("empty: got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNestedLoopsAcrossModes(t *testing.T) {
	// Абзацный цикл по заявкам, внутри — таблица с циклом по строкам документов заявки.
	file := makeDocx(t,
		p("{{#each applicants}}")+
			p("Участник {{@number}}: {{name}}")+
			table(row("{{#each docs}}{{title}}", "{{ok | check}}{{/each}}"))+
			p("{{/each}}"), nil)

	got := render(t, file, `{"applicants":[{"name":"Альфа","docs":[{"title":"Устав","ok":true},{"title":"Лицензия","ok":false}]},{"name":"Бета","docs":[]}]}`)
	want := "Участник 1: Альфа\nУстав | ☒\nЛицензия | ☐\nУчастник 2: Бета"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestHeadersAndFooters(t *testing.T) {
	ftr := `<?xml version="1.0" encoding="UTF-8"?><w:ftr xmlns:w="` + wordNS + `">` + p("Протокол {{number}}, {{ date | longdate:kk }}") + `</w:ftr>`
	file := makeDocx(t, p("Текст"), map[string]string{"word/footer1.xml": ftr})

	tmpl, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	out, err := tmpl.Render(map[string]any{"number": "0001", "date": "2026-10-20"})
	if err != nil {
		t.Fatal(err)
	}
	if got := documentText(t, out, "word/footer1.xml"); got != "Протокол 0001, 2026 жылғы 20 қазан" {
		t.Errorf("footer = %q", got)
	}
}

func TestRenderDoesNotChangeTemplate(t *testing.T) {
	file := makeDocx(t, p("{{#each items}}")+p("{{this}}")+p("{{/each}}"), nil)
	tmpl, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	a, err := tmpl.Render(map[string]any{"items": []any{"x", "y"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := tmpl.Render(map[string]any{"items": []any{"z"}})
	if err != nil {
		t.Fatal(err)
	}
	a2, err := tmpl.Render(map[string]any{"items": []any{"x", "y"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, a2) {
		t.Error("same template and data gave different files")
	}
	if got := documentText(t, b, "word/document.xml"); got != "z" {
		t.Errorf("second render = %q", got)
	}
}

func TestDataInjectionIsNotATag(t *testing.T) {
	file := makeDocx(t, p("Имя: {{name}}"), nil)
	got := render(t, file, `{"name":"{{secret}} <w:t>x</w:t>"}`)
	if got != "Имя: {{secret}} <w:t>x</w:t>" {
		t.Errorf("got %q", got)
	}
}

func TestDataErrors(t *testing.T) {
	file := makeDocx(t,
		p("{{customer.name}} {{ total | money }} {{ date | date }}")+
			p("{{#each lots}}{{name}}{{/each}}")+
			p("{{#each customer}}x{{/each}}")+
			p("{{ customer }}"), nil)
	tmpl, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := DecodeData([]byte(`{"customer":{"bin":"1"},"total":"много","lots":[{"name":"a"},{}]}`))
	_, err = tmpl.Render(data)

	var dErr *DataError
	if !errors.As(err, &dErr) {
		t.Fatalf("err = %v, want *DataError", err)
	}
	want := map[string]string{
		"customer.name": "нет в данных",
		"date":          "нет в данных",
		"lots[1].name":  "нет в данных",
	}
	got := make(map[string]string)
	for _, f := range dErr.Fields {
		got[f.Field] = f.Message
	}
	for field, msg := range want {
		if got[field] != msg {
			t.Errorf("%s: got %q, want %q (all: %+v)", field, got[field], msg, dErr.Fields)
		}
	}
	for _, field := range []string{"total", "customer"} {
		if got[field] == "" {
			t.Errorf("no error for %s (all: %+v)", field, dErr.Fields)
		}
	}
}

func TestTemplateErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"unclosed tag", p("Сумма {{ total"), "не закрыта"},
		{"unknown filter", p("{{ total | rubles }}"), "неизвестный форматтер"},
		{"filter arg", p("{{ total | words }}"), "нужен язык"},
		{"bad field", p("{{ total sum }}"), "неверное имя поля"},
		{"no close", p("{{#each lots}}{{name}}"), "не закрыт"},
		{"no open", p("{{name}}{{/if}}"), "без открывающей"},
		{"mismatch", p("{{#each lots}}{{/if}}"), "закрывает {{#each}}"},
		{"else outside", p("{{else}}"), "вне {{#each}}"},
		{"number outside", p("{{@number}}"), "только внутри"},
		{"up too far", p("{{#each a}}{{../../x}}{{/each}}"), "выходит за пределы"},
		{"paragraph with text", p("Лоты: {{#each lots}}") + p("{{name}}") + p("{{/each}}"), "в отдельном абзаце"},
		{"different levels", p("{{#each lots}}") + table(row("{{/each}}")), "на разном уровне"},
		{"else in first row", table(row("{{#each a}}{{else}}", "{{/each}}")), "в отдельной строке"},
		{"tag in tag", p("{{ a {{ b }} }}"), "метка внутри метки"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(makeDocx(t, tt.body, nil))
			var tErr *TemplateError
			if !errors.As(err, &tErr) {
				t.Fatalf("err = %v, want *TemplateError", err)
			}
			if !strings.Contains(tErr.Error(), tt.want) {
				t.Errorf("err = %v, want %q", tErr, tt.want)
			}
			if tErr.Problems[0].Part != "основной текст" {
				t.Errorf("part = %q", tErr.Problems[0].Part)
			}
		})
	}
}

func TestRejectedFiles(t *testing.T) {
	docm := makeDocx(t, p("x"), map[string]string{
		"[Content_Types].xml": strings.Replace(contentTypes, "%s", "application/vnd.ms-word.document.macroEnabled.main+xml", 1),
	})

	tests := []struct {
		name string
		file []byte
		want string
	}{
		{"not a zip", []byte("{\\rtf1 hello}"), "не открывается как архив"},
		{"macros", makeDocx(t, p("x"), map[string]string{"word/vbaProject.bin": "x"}), "макросы"},
		{"docm", docm, ".docm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.file)
			var tErr *TemplateError
			if !errors.As(err, &tErr) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want template error with %q", err, tt.want)
			}
		})
	}
}

func TestVariables(t *testing.T) {
	file := makeDocx(t,
		p("{{ number }} {{ total | money }} {{ total | moneywords:kk }}")+
			p("{{#each lots}}{{@number}} {{name}} {{#if note}}{{note}}{{/if}} {{../number}}{{/each}}")+
			table(row("{{#each members}}{{name}}", "{{#each votes}}{{decision}}{{/each}}{{/each}}")), nil)
	tmpl, err := Parse(file)
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[string]Variable)
	var order []string
	for _, v := range tmpl.Variables() {
		got[v.Path] = v
		order = append(order, v.Path)
	}
	wantOrder := []string{"number", "total", "lots", "lots[].name", "lots[].note", "members", "members[].name", "members[].votes", "members[].votes[].decision"}
	if strings.Join(order, ",") != strings.Join(wantOrder, ",") {
		t.Errorf("order = %v, want %v", order, wantOrder)
	}
	if v := got["total"]; strings.Join(v.Filters, ",") != "money,moneywords:kk" || strings.Join(v.Usage, ",") != "value" {
		t.Errorf("total = %+v", v)
	}
	if v := got["lots[].note"]; strings.Join(v.Usage, ",") != "if,value" {
		t.Errorf("lots[].note = %+v", v)
	}
}

func TestTableWithoutRowsIsRemoved(t *testing.T) {
	file := makeDocx(t, p("До")+table(row("{{#each docs}}{{title}}", "{{/each}}"))+p("После"), nil)

	if got := render(t, file, `{"docs":[]}`); got != "До\nПосле" {
		t.Errorf("empty list: got %q", got)
	}
	if got := render(t, file, `{"docs":[{"title":"Устав"}]}`); got != "До\nУстав | \nПосле" {
		t.Errorf("one item: got %q", got)
	}
}
