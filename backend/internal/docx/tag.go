package docx

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

type tagKind int

const (
	tagValue tagKind = iota // {{ path | filter }}
	tagOpen                 // {{#each path}}, {{#if path}}
	tagElse                 // {{else}}
	tagClose                // {{/each}}, {{/if}}
)

// path — ссылка на значение в данных: ../ поднимается в цикл выше, special — @number, @index, this.
type path struct {
	up      int
	special string
	fields  []string
}

func (p path) String() string {
	s := strings.Repeat("../", p.up)
	if p.special != "" {
		return s + p.special
	}
	return s + strings.Join(p.fields, ".")
}

type filter struct {
	name string
	arg  string
}

type tagExpr struct {
	kind    tagKind
	block   string // each или if для open и close
	path    path
	filters []filter
}

// filterArgs — какие форматтеры есть и что они принимают после двоеточия.
var filterArgs = map[string]func(arg string) error{
	"money":      noArg,
	"number":     noArg,
	"date":       noArg,
	"datetime":   noArg,
	"longdate":   localeArg,
	"words":      localeArg,
	"moneywords": localeArg,
	"iin":        noArg,
	"bin":        noArg,
	"check":      noArg,
	"upper":      noArg,
	"lower":      noArg,
	"capfirst":   noArg,
	"default":    func(string) error { return nil },
}

func noArg(arg string) error {
	if arg != "" {
		return errors.New("у этого форматтера нет параметра")
	}
	return nil
}

func localeArg(arg string) error {
	if arg != "ru" && arg != "kk" {
		return errors.New("нужен язык: ru или kk, например words:ru")
	}
	return nil
}

// parseTag разбирает текст метки без фигурных скобок.
func parseTag(raw string) (tagExpr, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return tagExpr{}, errors.New("пустая метка")
	}
	if strings.Contains(s, "{{") || strings.Contains(s, "}}") {
		return tagExpr{}, errors.New("метка внутри метки")
	}

	switch {
	case s == "else":
		return tagExpr{kind: tagElse}, nil
	case s == "/each" || s == "/if":
		return tagExpr{kind: tagClose, block: s[1:]}, nil
	case strings.HasPrefix(s, "#"):
		name, rest, _ := strings.Cut(s[1:], " ")
		if name != "each" && name != "if" {
			return tagExpr{}, fmt.Errorf("неизвестный блок #%s: есть только #each и #if", name)
		}
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return tagExpr{}, fmt.Errorf("после #%s нужно поле", name)
		}
		if strings.Contains(rest, "|") {
			return tagExpr{}, fmt.Errorf("в #%s форматтеры не используются", name)
		}
		p, err := parsePath(rest)
		if err != nil {
			return tagExpr{}, err
		}
		return tagExpr{kind: tagOpen, block: name, path: p}, nil
	}

	parts := strings.Split(s, "|")
	p, err := parsePath(strings.TrimSpace(parts[0]))
	if err != nil {
		return tagExpr{}, err
	}
	expr := tagExpr{kind: tagValue, path: p}
	for _, part := range parts[1:] {
		name, arg, _ := strings.Cut(strings.TrimSpace(part), ":")
		name, arg = strings.TrimSpace(name), strings.TrimSpace(arg)
		check, ok := filterArgs[name]
		if !ok {
			return tagExpr{}, fmt.Errorf("неизвестный форматтер %q", name)
		}
		if err := check(arg); err != nil {
			return tagExpr{}, fmt.Errorf("форматтер %s: %w", name, err)
		}
		expr.filters = append(expr.filters, filter{name: name, arg: arg})
	}
	return expr, nil
}

func parsePath(s string) (path, error) {
	var p path
	for strings.HasPrefix(s, "../") {
		p.up++
		s = s[3:]
	}
	switch s {
	case "@number", "@index", "this":
		p.special = s
		return p, nil
	case "":
		return p, errors.New("после ../ нужно поле")
	}
	for f := range strings.SplitSeq(s, ".") {
		if !validField(f) {
			return p, fmt.Errorf("неверное имя поля %q: буквы, цифры, _ и -, вложенные поля через точку", s)
		}
		p.fields = append(p.fields, f)
	}
	return p, nil
}

func validField(f string) bool {
	if f == "" {
		return false
	}
	for _, r := range f {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}
