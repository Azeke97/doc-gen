package docx

import (
	"slices"
	"sort"
	"strings"
)

// Variable — поле данных, которое использует шаблон. Path — в виде «lots[].name»:
// [] означает элемент списка из {{#each lots}}.
type Variable struct {
	Path    string   `json:"path"`
	Usage   []string `json:"usage"`             // value, each, if
	Filters []string `json:"filters,omitempty"` // форматтеры, с которыми поле выводится
}

// Variables перечисляет поля данных, которые использует шаблон, в порядке первого появления.
func (t *Template) Variables() []Variable {
	var order []string
	vars := make(map[string]*Variable)
	add := func(path, usage string, filters []filter) {
		if path == "" {
			return
		}
		v, ok := vars[path]
		if !ok {
			v = &Variable{Path: path}
			vars[path] = v
			order = append(order, path)
		}
		if !slices.Contains(v.Usage, usage) {
			v.Usage = append(v.Usage, usage)
		}
		for _, f := range filters {
			name := f.name
			if f.arg != "" {
				name += ":" + f.arg
			}
			if !slices.Contains(v.Filters, name) {
				v.Filters = append(v.Filters, name)
			}
		}
	}

	// Сначала основной текст, потом колонтитулы — так порядок совпадает с тем, как читают документ.
	names := make([]string, 0, len(t.parts))
	for name := range t.parts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == "word/document.xml") != (names[j] == "word/document.xml") {
			return names[i] == "word/document.xml"
		}
		return names[i] < names[j]
	})

	for _, name := range names {
		var scopes []string // путь списка для каждого открытого блока; "" — корень
		cur := func() string {
			if len(scopes) == 0 {
				return ""
			}
			return scopes[len(scopes)-1]
		}
		for _, tg := range t.parts[name].tags {
			e := tg.expr
			switch e.kind {
			case tagValue:
				add(display(scopes, e.path), "value", e.filters)
			case tagOpen:
				p := display(scopes, e.path)
				add(p, e.block, nil)
				if e.block == "each" {
					scopes = append(scopes, p+"[]")
				} else {
					scopes = append(scopes, cur())
				}
			case tagClose:
				if len(scopes) > 0 {
					scopes = scopes[:len(scopes)-1]
				}
			}
		}
	}

	out := make([]Variable, 0, len(order))
	for _, p := range order {
		out = append(out, *vars[p])
	}
	return out
}

// display строит путь поля от корня данных с учётом циклов вокруг и ../.
// Для @number, @index и this поле не нужно: возвращается "".
func display(scopes []string, pt path) string {
	if pt.special != "" {
		return ""
	}
	// Каждый ../ выходит из одного цикла; #if области не меняет, поэтому считаем только списки.
	lists := []string{""}
	for _, s := range scopes {
		if s != lists[len(lists)-1] {
			lists = append(lists, s)
		}
	}
	base := lists[max(len(lists)-1-pt.up, 0)]
	field := strings.Join(pt.fields, ".")
	if base == "" {
		return field
	}
	return base + "." + field
}
