package docx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	pathpkg "path"
	"regexp"
	"strings"

	"github.com/beevik/etree"
)

const (
	// MaxFileSize — предел размера файла шаблона.
	MaxFileSize = 10 << 20
	// MaxUnpackedSize — предел суммарного размера распакованного шаблона: защита от zip-бомб.
	MaxUnpackedSize = 50 << 20

	wordNS          = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	mainContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
)

// Части документа, где ищутся метки: основной текст и колонтитулы.
var partPattern = regexp.MustCompile(`^word/(document|header\d*|footer\d*)\.xml$`)

// Template — разобранный и проверенный шаблон. Render его не меняет, поэтому один
// Template можно отрисовывать сколько угодно раз, в том числе одновременно.
type Template struct {
	files []*zip.File
	parts map[string]*part // по имени файла в архиве
}

// part — XML одной части документа и найденные в ней метки и блоки.
type part struct {
	name    string
	title   string
	doc     *etree.Document
	w       string // префикс пространства имён WordprocessingML в этой части
	tags    []*tag
	blocks  map[*etree.Element]*block // по открывающему якорю блока
	anchors map[*etree.Element]*block // все якоря: элемент не может быть якорем двух блоков
	values  map[*etree.Element]*tag   // прогон с меткой значения
	drop    map[*etree.Element]bool   // прогоны с метками блоков внутри строк таблицы
}

// tag — метка в документе. После нормализации каждая метка — отдельный прогон (w:r).
type tag struct {
	expr tagExpr
	raw  string
	run  *etree.Element
	para *etree.Element
}

// Parse открывает DOCX и проверяет шаблон. Ошибки шаблона — *TemplateError со всеми
// найденными проблемами.
func Parse(file []byte) (*Template, error) {
	if len(file) > MaxFileSize {
		return nil, templateError("", "", "файл больше %d МиБ", MaxFileSize>>20)
	}
	zr, err := zip.NewReader(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		return nil, templateError("", "", "это не DOCX: файл не открывается как архив Word (старый .doc или файл с паролем не подходят)")
	}

	var unpacked uint64
	contents := make(map[string][]byte)
	for _, f := range zr.File {
		unpacked += f.UncompressedSize64
		if unpacked > MaxUnpackedSize {
			return nil, templateError("", "", "распакованный документ больше %d МиБ", MaxUnpackedSize>>20)
		}
		if strings.EqualFold(pathpkg.Base(f.Name), "vbaProject.bin") {
			return nil, templateError("", "", "в документе есть макросы: нужен .docx без макросов")
		}
		if f.Name == "[Content_Types].xml" || partPattern.MatchString(f.Name) {
			b, err := readLimited(f)
			if err != nil {
				return nil, templateError("", "", "повреждённый файл %s: %v", f.Name, err)
			}
			contents[f.Name] = b
		}
	}

	if err := checkContentType(contents["[Content_Types].xml"]); err != nil {
		return nil, err
	}
	if _, ok := contents["word/document.xml"]; !ok {
		return nil, templateError("", "", "это не DOCX: нет основного текста word/document.xml")
	}

	t := &Template{files: zr.File, parts: make(map[string]*part)}
	var problems []Problem
	for name, b := range contents {
		if name == "[Content_Types].xml" {
			continue
		}
		p, err := parsePart(name, b)
		if err != nil {
			return nil, err
		}
		problems = append(problems, p.prepare()...)
		t.parts[name] = p
	}
	if len(problems) > 0 {
		return nil, &TemplateError{Problems: problems}
	}
	return t, nil
}

func readLimited(f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()

	b, err := io.ReadAll(io.LimitReader(r, MaxUnpackedSize+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(b)) != f.UncompressedSize64 {
		return nil, errors.New("размер не совпадает с заявленным")
	}
	return b, nil
}

// checkContentType отличает обычный документ от шаблона Word (.dotx) и документа с макросами (.docm).
func checkContentType(b []byte) error {
	doc := etree.NewDocument()
	if b == nil || doc.ReadFromBytes(b) != nil || doc.Root() == nil {
		return templateError("", "", "это не DOCX: нет описания содержимого [Content_Types].xml")
	}
	for _, o := range doc.Root().ChildElements() {
		if o.Tag == "Override" && o.SelectAttrValue("PartName", "") == "/word/document.xml" {
			ct := o.SelectAttrValue("ContentType", "")
			if ct == mainContentType {
				return nil
			}
			if strings.Contains(ct, "macroEnabled") {
				return templateError("", "", "это документ с макросами (.docm): нужен .docx")
			}
			return templateError("", "", "это не обычный документ Word (%s): сохраните его как .docx", ct)
		}
	}
	return templateError("", "", "это не DOCX: в [Content_Types].xml не описан основной текст")
}

func parsePart(name string, b []byte) (*part, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(b); err != nil || doc.Root() == nil {
		return nil, templateError(partTitle(name), "", "повреждённый XML в %s", name)
	}
	p := &part{
		name:    name,
		title:   partTitle(name),
		doc:     doc,
		w:       "w",
		blocks:  make(map[*etree.Element]*block),
		anchors: make(map[*etree.Element]*block),
		values:  make(map[*etree.Element]*tag),
		drop:    make(map[*etree.Element]bool),
	}
	for _, a := range doc.Root().Attr {
		if a.Space == "xmlns" && a.Value == wordNS {
			p.w = a.Key
		}
	}
	return p, nil
}

func partTitle(name string) string {
	switch {
	case strings.HasPrefix(name, "word/header"):
		return "верхний колонтитул"
	case strings.HasPrefix(name, "word/footer"):
		return "нижний колонтитул"
	default:
		return "основной текст"
	}
}

func (p *part) is(e *etree.Element, tag string) bool {
	return e != nil && e.Space == p.w && e.Tag == tag
}

// ancestor возвращает ближайшего предка e (или сам e) с тегом tag.
func (p *part) ancestor(e *etree.Element, tag string) *etree.Element {
	for ; e != nil; e = e.Parent() {
		if p.is(e, tag) {
			return e
		}
	}
	return nil
}

// prepare нормализует метки, находит их и строит блоки. Возвращает проблемы шаблона.
func (p *part) prepare() []Problem {
	var problems []Problem
	for _, para := range p.paragraphs(p.doc.Root()) {
		if err := p.mergeTags(para); err != nil {
			problems = append(problems, Problem{Part: p.title, Context: p.text(para), Message: err.Error()})
		}
	}
	if len(problems) > 0 {
		return problems
	}

	p.collectTags(p.doc.Root())
	for _, t := range p.tags {
		expr, err := parseTag(strings.TrimSuffix(strings.TrimPrefix(t.raw, "{{"), "}}"))
		if err != nil {
			problems = append(problems, p.problem(t, "%s: %v", t.raw, err))
			continue
		}
		t.expr = expr
	}
	if len(problems) > 0 {
		return problems
	}
	return p.buildBlocks()
}

func (p *part) problem(t *tag, format string, args ...any) Problem {
	return Problem{Part: p.title, Context: p.text(t.para), Message: fmt.Sprintf(format, args...)}
}

// paragraphs возвращает все абзацы части, включая абзацы в таблицах и надписях.
func (p *part) paragraphs(root *etree.Element) []*etree.Element {
	var out []*etree.Element
	var walk func(e *etree.Element)
	walk = func(e *etree.Element) {
		if p.is(e, "p") {
			out = append(out, e)
		}
		for _, c := range e.ChildElements() {
			walk(c)
		}
	}
	walk(root)
	return out
}

// texts — текстовые элементы (w:t) абзаца по порядку, без абзацев, вложенных в надписи.
func (p *part) texts(para *etree.Element) []*etree.Element {
	var out []*etree.Element
	var walk func(e *etree.Element)
	walk = func(e *etree.Element) {
		for _, c := range e.ChildElements() {
			switch {
			case p.is(c, "p"):
				continue
			case p.is(c, "t"):
				out = append(out, c)
			default:
				walk(c)
			}
		}
	}
	walk(para)
	return out
}

func (p *part) text(para *etree.Element) string {
	if para == nil {
		return ""
	}
	var b strings.Builder
	for _, t := range p.texts(para) {
		b.WriteString(t.Text())
	}
	s := strings.TrimSpace(b.String())
	if r := []rune(s); len(r) > 100 {
		s = string(r[:100]) + "…"
	}
	return s
}

// mergeTags собирает каждую метку абзаца в один w:t, а потом выносит метки в отдельные
// прогоны. Word разбивает текст на прогоны как угодно (правка, проверка орфографии),
// и «{{ name }}» в XML может оказаться в трёх кусках.
func (p *part) mergeTags(para *etree.Element) error {
	for {
		texts := p.texts(para)
		var full strings.Builder
		starts := make([]int, len(texts))
		for i, t := range texts {
			starts[i] = full.Len()
			full.WriteString(t.Text())
		}
		s := full.String()

		fixed := false
		for pos := 0; ; {
			open := strings.Index(s[pos:], "{{")
			if open < 0 {
				break
			}
			open += pos
			end := strings.Index(s[open:], "}}")
			if end < 0 {
				return errors.New("метка не закрыта: после {{ нет }}")
			}
			end += open + 2
			first, last := owner(starts, open), owner(starts, end-1)
			if first != last {
				moveText(texts, starts, first, last, open, end)
				fixed = true
				break
			}
			pos = end
		}
		if !fixed {
			break
		}
	}

	for _, t := range p.texts(para) {
		if strings.Contains(t.Text(), "{{") {
			if err := p.splitRun(t); err != nil {
				return err
			}
		}
	}
	return nil
}

// owner — индекс текстового элемента, которому принадлежит символ на позиции pos.
func owner(starts []int, pos int) int {
	i := 0
	for j, s := range starts {
		if s <= pos {
			i = j
		}
	}
	return i
}

// moveText переносит метку [open, end) целиком в первый текстовый элемент, где она начинается.
func moveText(texts []*etree.Element, starts []int, first, last, open, end int) {
	var tagText strings.Builder
	var prefix string
	for i := first; i <= last; i++ {
		t := texts[i].Text()
		from, to := max(open-starts[i], 0), min(end-starts[i], len(t))
		tagText.WriteString(t[from:to])
		if i == first {
			prefix = t[:from] // метка начинается здесь и продолжается дальше: хвоста нет
		} else {
			setText(texts[i], t[to:])
		}
	}
	setText(texts[first], prefix+tagText.String())
}

func setText(t *etree.Element, s string) {
	t.SetText(s)
	if t.SelectAttr("xml:space") == nil {
		t.CreateAttr("xml:space", "preserve")
	}
}

// splitRun разрезает прогон с текстом t так, чтобы каждая метка стала отдельным прогоном
// с тем же форматированием: так метку можно заменить или убрать, не трогая соседний текст.
func (p *part) splitRun(t *etree.Element) error {
	run := t.Parent()
	if !p.is(run, "r") {
		return errors.New("метка в неподдерживаемом месте документа")
	}
	var rPr *etree.Element
	for _, c := range run.ChildElements() {
		if p.is(c, "rPr") {
			rPr = c
		}
	}
	shell := func() *etree.Element {
		r := etree.NewElement(run.FullTag())
		r.Attr = append(r.Attr, run.Attr...)
		if rPr != nil {
			r.AddChild(rPr.Copy())
		}
		return r
	}

	var runs []*etree.Element
	cur, empty := shell(), true
	flush := func() {
		if !empty {
			runs = append(runs, cur)
		}
		cur, empty = shell(), true
	}
	for _, c := range run.Child {
		el, ok := c.(*etree.Element)
		if ok && el == rPr {
			continue
		}
		if !ok || el != t {
			cur.AddChild(copyToken(c))
			empty = empty && !ok
			continue
		}
		for _, piece := range splitTags(t.Text()) {
			if piece == "" {
				continue
			}
			if strings.HasPrefix(piece, "{{") {
				flush()
				tr := shell()
				tr.AddChild(newText(p.w, piece))
				runs = append(runs, tr)
				continue
			}
			cur.AddChild(newText(p.w, piece))
			empty = false
		}
	}
	flush()

	parent := run.Parent()
	idx := run.Index()
	parent.RemoveChildAt(idx)
	for i, r := range runs {
		parent.InsertChildAt(idx+i, r)
	}
	return nil
}

// splitTags делит текст на куски: обычный текст и метки {{...}} целиком.
func splitTags(s string) []string {
	var out []string
	for {
		open := strings.Index(s, "{{")
		if open < 0 {
			return append(out, s)
		}
		end := strings.Index(s[open:], "}}")
		if end < 0 {
			return append(out, s)
		}
		end += open + 2
		out = append(out, s[:open], s[open:end])
		s = s[end:]
	}
}

func newText(w, s string) *etree.Element {
	t := etree.NewElement(w + ":t")
	t.CreateAttr("xml:space", "preserve")
	t.SetText(s)
	return t
}

// copyToken копирует узел XML вместе с потомками.
func copyToken(t etree.Token) etree.Token {
	switch v := t.(type) {
	case *etree.Element:
		return v.Copy()
	case *etree.CharData:
		return etree.NewText(v.Data)
	case *etree.Comment:
		return etree.NewComment(v.Data)
	case *etree.ProcInst:
		return etree.NewProcInst(v.Target, v.Inst)
	default:
		return etree.NewText("")
	}
}

// collectTags находит прогоны-метки в порядке документа.
func (p *part) collectTags(root *etree.Element) {
	var walk func(e *etree.Element)
	walk = func(e *etree.Element) {
		if p.is(e, "r") {
			if raw, ok := p.tagRun(e); ok {
				p.tags = append(p.tags, &tag{raw: raw, run: e, para: p.ancestor(e, "p")})
				return
			}
		}
		for _, c := range e.ChildElements() {
			walk(c)
		}
	}
	walk(root)
}

// tagRun — это прогон, в котором только метка (результат splitRun)?
func (p *part) tagRun(r *etree.Element) (string, bool) {
	var text string
	n := 0
	for _, c := range r.ChildElements() {
		if p.is(c, "rPr") {
			continue
		}
		n++
		if !p.is(c, "t") {
			return "", false
		}
		text = c.Text()
	}
	if n == 1 && strings.HasPrefix(text, "{{") && strings.HasSuffix(text, "}}") {
		return text, true
	}
	return "", false
}
