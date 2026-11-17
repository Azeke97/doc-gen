package docx

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/beevik/etree"

	"docgen/internal/format"
)

// DecodeData читает JSON с данными: корень — объект, числа остаются json.Number,
// чтобы суммы не теряли точность.
func DecodeData(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var data map[string]any
	if err := dec.Decode(&data); err != nil {
		return nil, fmt.Errorf("data is not a JSON object: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("data must contain a single JSON object")
	}
	if data == nil {
		return nil, fmt.Errorf("data is not a JSON object")
	}
	return data, nil
}

// zipTime — время изменения файлов в результате: одно и то же, чтобы один шаблон
// и одни данные давали байт в байт одинаковый DOCX.
var zipTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// Render подставляет данные в шаблон и возвращает DOCX. Данные не подходят — *DataError
// со всеми найденными ошибками (не больше maxFieldErrors).
func (t *Template) Render(data map[string]any) ([]byte, error) {
	r := &renderer{}
	out := make(map[string][]byte, len(t.parts))
	for name, p := range t.parts {
		b, err := r.part(p, data)
		if err != nil {
			return nil, err
		}
		out[name] = b
	}
	if len(r.errs) > 0 {
		return nil, &DataError{Fields: r.errs}
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range t.files {
		content, changed := out[f.Name]
		if !changed {
			if err := zw.Copy(f); err != nil {
				return nil, fmt.Errorf("copy %s: %w", f.Name, err)
			}
			continue
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: zipTime})
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(w, bytes.NewReader(content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// scope — значение, на которое указывают пути: корень данных или элемент цикла.
type scope struct {
	val    any
	parent *scope
	index  int
	label  string // путь для сообщений об ошибках: "lots[2]"
}

type renderer struct {
	p      *part
	errs   []FieldError
	seen   map[string]bool
	active map[*block]bool
}

func (r *renderer) part(p *part, data map[string]any) ([]byte, error) {
	r.p = p
	r.active = make(map[*block]bool)
	src := p.doc
	dst := etree.NewDocument()
	dst.WriteSettings = src.WriteSettings
	root := &scope{val: data}

	for _, tok := range src.Child {
		el, ok := tok.(*etree.Element)
		if !ok {
			dst.AddChild(copyToken(tok))
			continue
		}
		n := shallow(el)
		r.span(el.Child, 0, len(el.Child), n, root)
		dst.AddChild(n)
	}
	return dst.WriteToBytes()
}

func shallow(e *etree.Element) *etree.Element {
	n := etree.NewElement(e.FullTag())
	n.Attr = append(n.Attr, e.Attr...)
	return n
}

// span копирует узлы kids[from:to] в dst, раскрывая метки и блоки.
func (r *renderer) span(kids []etree.Token, from, to int, dst *etree.Element, sc *scope) {
	for i := from; i < to; i++ {
		el, ok := kids[i].(*etree.Element)
		if !ok {
			dst.AddChild(copyToken(kids[i]))
			continue
		}
		if b := r.p.blocks[el]; b != nil && !r.active[b] {
			i = r.block(b, kids, i, dst, sc)
			continue
		}
		if r.p.drop[el] {
			continue
		}
		if t := r.p.values[el]; t != nil {
			dst.AddChild(r.value(el, t, sc))
			continue
		}
		n := shallow(el)
		r.span(el.Child, 0, len(el.Child), n, sc)
		dst.AddChild(n)
	}
}

// block раскрывает блок, начинающийся с kids[start]. Возвращает индекс его последнего якоря.
func (r *renderer) block(b *block, kids []etree.Token, start int, dst *etree.Element, sc *scope) int {
	mid, end := -1, -1
	for i := start; i < len(kids); i++ {
		if kids[i] == b.middle {
			mid = i
		}
		if kids[i] == b.last {
			end = i
			break
		}
	}

	// Диапазоны содержимого: в тексте и абзацах якоря — сами метки, они не выводятся;
	// в таблице якоря — строки с содержимым, из них убираются только прогоны с метками.
	mainFrom, mainTo, elseFrom, elseTo := start+1, end, end, end
	if mid >= 0 {
		mainTo, elseFrom = mid, mid+1
	}
	if b.mode == modeRows {
		mainFrom = start
		if mid >= 0 {
			elseFrom, elseTo = mid, end+1
		} else {
			mainTo = end + 1
		}
	}

	r.active[b] = true
	defer delete(r.active, b)

	val, label, ok := r.resolve(b.open.expr.path, sc)
	if !ok {
		return end
	}
	switch b.open.expr.block {
	case "each":
		items, isList := val.([]any)
		if !isList {
			if val != nil {
				r.fail(label, "ожидается список (массив) для {{#each}}")
			}
		}
		if len(items) == 0 {
			r.span(kids, elseFrom, elseTo, dst, sc)
			return end
		}
		for i, item := range items {
			r.span(kids, mainFrom, mainTo, dst, &scope{val: item, parent: sc, index: i, label: fmt.Sprintf("%s[%d]", label, i)})
		}
	case "if":
		if truthy(val) {
			r.span(kids, mainFrom, mainTo, dst, sc)
		} else {
			r.span(kids, elseFrom, elseTo, dst, sc)
		}
	}
	return end
}

// value — прогон метки с подставленным значением и прежним форматированием.
func (r *renderer) value(run *etree.Element, t *tag, sc *scope) *etree.Element {
	n := shallow(run)
	for _, c := range run.ChildElements() {
		if r.p.is(c, "rPr") {
			n.AddChild(c.Copy())
		}
	}

	text, ok := r.evaluate(t.expr, sc)
	if !ok {
		return n
	}
	// Перевод строки в данных — разрыв строки в Word, табуляция — табуляция.
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			n.AddChild(etree.NewElement(r.p.w + ":br"))
		}
		for j, piece := range strings.Split(line, "\t") {
			if j > 0 {
				n.AddChild(etree.NewElement(r.p.w + ":tab"))
			}
			if piece != "" {
				n.AddChild(newText(r.p.w, piece))
			}
		}
	}
	return n
}

func (r *renderer) evaluate(e tagExpr, sc *scope) (string, bool) {
	hasDefault := false
	for _, f := range e.filters {
		hasDefault = hasDefault || f.name == "default"
	}

	val, label, ok := r.lookup(e.path, sc)
	if !ok && !hasDefault {
		r.fail(label, "нет в данных")
		return "", false
	}

	cur := val
	for _, f := range e.filters {
		out, err := applyFilter(f, cur)
		if err != nil {
			r.fail(label, fmt.Sprintf("%s: %v", f.name, err))
			return "", false
		}
		cur = out
	}
	s, err := toText(cur)
	if err != nil {
		r.fail(label, err.Error())
		return "", false
	}
	return s, true
}

// resolve — как lookup, но отсутствие значения сразу записывается в ошибки.
func (r *renderer) resolve(pt path, sc *scope) (any, string, bool) {
	val, label, ok := r.lookup(pt, sc)
	if !ok {
		r.fail(label, "нет в данных")
	}
	return val, label, ok
}

// lookup находит значение по пути. ok=false — значения нет в данных; label — путь для сообщения.
func (r *renderer) lookup(pt path, sc *scope) (any, string, bool) {
	for range pt.up {
		sc = sc.parent // глубина ../ проверена при разборе
	}
	switch pt.special {
	case "@number":
		return json.Number(strconv.Itoa(sc.index + 1)), sc.label, true
	case "@index":
		return json.Number(strconv.Itoa(sc.index)), sc.label, true
	case "this":
		return sc.val, sc.label, true
	}

	cur, label := sc.val, sc.label
	for _, f := range pt.fields {
		if label == "" {
			label = f
		} else {
			label += "." + f
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, label, false
		}
		if cur, ok = m[f]; !ok {
			return nil, label, false
		}
	}
	return cur, label, true
}

func (r *renderer) fail(field, msg string) {
	if r.seen == nil {
		r.seen = make(map[string]bool)
	}
	key := field + "\x00" + msg
	if r.seen[key] || len(r.errs) >= maxFieldErrors {
		return
	}
	r.seen[key] = true
	r.errs = append(r.errs, FieldError{Field: field, Message: msg})
}

// truthy — ложь: false, null, пустая строка, 0, пустой список или объект.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case json.Number:
		f, err := x.Float64()
		return err != nil || f != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	default:
		return true
	}
}

// toText выводит простое значение; объект или список вывести нельзя.
func toText(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case json.Number:
		return x.String(), nil
	case bool:
		if x {
			return "да", nil
		}
		return "нет", nil
	case []any:
		return "", fmt.Errorf("это список: выводите его через {{#each}}")
	case map[string]any:
		return "", fmt.Errorf("это объект: выводите его поля, например %s", "{{ поле.имя }}")
	default:
		return fmt.Sprint(x), nil
	}
}

func applyFilter(f filter, v any) (any, error) {
	switch f.name {
	case "money":
		return format.Money(v)
	case "number":
		return format.Number(v)
	case "date":
		return format.Date(v)
	case "datetime":
		return format.DateTime(v)
	case "longdate":
		return format.LongDate(v, f.arg)
	case "words":
		return format.Words(v, f.arg)
	case "moneywords":
		return format.MoneyWords(v, f.arg)
	case "iin", "bin":
		return format.KzID(v)
	case "check":
		return format.Check(v)
	case "default":
		if s, _ := toText(v); s == "" {
			return f.arg, nil
		}
		return v, nil
	}

	s, err := toText(v)
	if err != nil {
		return nil, err
	}
	switch f.name {
	case "upper":
		return strings.ToUpper(s), nil
	case "lower":
		return strings.ToLower(s), nil
	case "capfirst":
		return format.CapFirst(s), nil
	}
	return nil, fmt.Errorf("неизвестный форматтер")
}
