package docx

import (
	"strings"

	"github.com/beevik/etree"
)

// mode — что повторяет или убирает блок.
type mode int

const (
	modeInline    mode = iota // часть абзаца: метки в одном абзаце
	modeParagraph             // целые абзацы между абзацами с метками
	modeRows                  // строки таблицы: метки в разных ячейках
)

// block — #each или #if. first, middle, last — «якоря» блока: соседние элементы одного
// родителя (прогоны, абзацы или строки таблицы), между которыми лежит содержимое.
type block struct {
	open, els, close *tag
	mode             mode
	first, middle    *etree.Element // middle — якорь {{else}}, nil если его нет
	last             *etree.Element
}

// buildBlocks связывает открывающие и закрывающие метки, определяет вид каждого блока
// и проверяет пути (../ и @number). Возвращает проблемы шаблона.
func (p *part) buildBlocks() []Problem {
	var problems []Problem
	type frame struct {
		b     *block
		depth int // число #each, открытых снаружи этого блока и в нём
	}
	var stack []frame
	depth := func() int {
		if len(stack) == 0 {
			return 0
		}
		return stack[len(stack)-1].depth
	}
	checkPath := func(t *tag, pt path) {
		if pt.up > depth() {
			problems = append(problems, p.problem(t, "%s: ../ выходит за пределы циклов (циклов вокруг: %d)", t.raw, depth()))
		}
		if (pt.special == "@number" || pt.special == "@index") && depth() == 0 {
			problems = append(problems, p.problem(t, "%s: %s бывает только внутри {{#each}}", t.raw, pt.special))
		}
	}

	for _, t := range p.tags {
		switch t.expr.kind {
		case tagValue:
			checkPath(t, t.expr.path)
			p.values[t.run] = t
		case tagOpen:
			checkPath(t, t.expr.path)
			d := depth()
			if t.expr.block == "each" {
				d++
			}
			stack = append(stack, frame{b: &block{open: t}, depth: d})
		case tagElse:
			if len(stack) == 0 {
				problems = append(problems, p.problem(t, "{{else}} вне {{#each}} или {{#if}}"))
				continue
			}
			b := stack[len(stack)-1].b
			if b.els != nil {
				problems = append(problems, p.problem(t, "второй {{else}} в одном блоке {{#%s}}", b.open.expr.block))
				continue
			}
			b.els = t
		case tagClose:
			if len(stack) == 0 {
				problems = append(problems, p.problem(t, "%s без открывающей {{#%s}}", t.raw, t.expr.block))
				continue
			}
			b := stack[len(stack)-1].b
			stack = stack[:len(stack)-1]
			if b.open.expr.block != t.expr.block {
				problems = append(problems, p.problem(t, "%s закрывает {{#%s}}, открытый в «%s»", t.raw, b.open.expr.block, p.text(b.open.para)))
				continue
			}
			b.close = t
			if err := p.place(b); err != "" {
				problems = append(problems, p.problem(b.open, "%s … %s: %s", b.open.raw, t.raw, err))
			}
		}
	}
	for _, f := range stack {
		problems = append(problems, p.problem(f.b.open, "%s не закрыт: нет {{/%s}}", f.b.open.raw, f.b.open.expr.block))
	}
	return problems
}

// place определяет вид блока по положению меток и запоминает якоря.
// Возвращает описание ошибки или пустую строку.
func (p *part) place(b *block) string {
	o, c := b.open, b.close
	var anchors [3]*etree.Element // first, middle, last

	switch {
	case o.para == c.para:
		b.mode = modeInline
		anchors = [3]*etree.Element{o.run, nil, c.run}
		if b.els != nil {
			anchors[1] = b.els.run
		}
		for _, a := range anchors {
			if a != nil && a.Parent() != o.run.Parent() {
				return "метки блока в одном абзаце, но на разном уровне (например, одна внутри гиперссылки)"
			}
		}

	case p.inDifferentCells(o.para, c.para):
		b.mode = modeRows
		anchors = [3]*etree.Element{p.row(o.para), nil, p.row(c.para)}
		if b.els != nil {
			anchors[1] = p.row(b.els.para)
			if anchors[1] == anchors[0] {
				return "{{else}} в строке таблицы с открывающей меткой: для строк «иначе» его ставят в отдельной строке"
			}
		}
		for _, a := range anchors {
			if a != nil && a.Parent() != anchors[0].Parent() {
				return "метки блока в разных таблицах: цикл по строкам должен быть в одной таблице"
			}
		}
		for _, t := range []*tag{o, b.els, c} {
			if t != nil {
				p.drop[t.run] = true
			}
		}

	default:
		b.mode = modeParagraph
		anchors = [3]*etree.Element{o.para, nil, c.para}
		if b.els != nil {
			anchors[1] = b.els.para
		}
		for _, a := range anchors {
			if a != nil && a.Parent() != o.para.Parent() {
				return "метки блока на разном уровне документа (например, одна в таблице, другая в основном тексте)"
			}
		}
		for _, t := range []*tag{o, b.els, c} {
			if t != nil && p.otherText(t) {
				return "блок занимает несколько абзацев, поэтому " + t.raw + " должен стоять в отдельном абзаце без другого текста"
			}
		}
	}

	for _, a := range anchors {
		if a == nil {
			continue
		}
		if other, ok := p.anchors[a]; ok && other != b {
			return "блок пересекается с другим блоком: у них общая строка или абзац с метками"
		}
	}
	b.first, b.middle, b.last = anchors[0], anchors[1], anchors[2]
	for _, a := range anchors {
		if a != nil {
			p.anchors[a] = b
		}
	}
	p.blocks[b.first] = b
	return ""
}

// inDifferentCells — оба абзаца в таблице, но в разных ячейках.
func (p *part) inDifferentCells(a, b *etree.Element) bool {
	ca, cb := p.ancestor(a, "tc"), p.ancestor(b, "tc")
	return ca != nil && cb != nil && ca != cb
}

func (p *part) row(para *etree.Element) *etree.Element {
	return p.ancestor(para, "tr")
}

// otherText — есть ли в абзаце метки t текст кроме неё самой.
func (p *part) otherText(t *tag) bool {
	var b strings.Builder
	for _, x := range p.texts(t.para) {
		if x.Parent() != t.run {
			b.WriteString(x.Text())
		}
	}
	return strings.TrimSpace(b.String()) != ""
}
