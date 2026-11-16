package htmlengine

import (
	"encoding/json"
	"fmt"
	"html/template"
	"maps"
	"strings"

	"docgen/internal/format"
)

// Funcs возвращает функции шаблонов. Один набор и для проверки при загрузке, и для генерации.
// Сравнения eq, ne, lt, le, gt, ge заменены: встроенные не умеют сравнивать json.Number с числом.
func Funcs() template.FuncMap {
	funcs := template.FuncMap{
		"money":      format.Money,
		"number":     format.Number,
		"date":       format.Date,
		"datetime":   format.DateTime,
		"longdate":   format.LongDate,
		"words":      format.Words,
		"moneywords": format.MoneyWords,
		"capfirst":   format.CapFirst,
		"bin":        format.KzID,
		"iin":        format.KzID,
		"check":      format.Check,
		"tr":         format.Tr,
		"dict":       dict,
		"inc":        func(i int) int { return i + 1 },

		"eq": eq,
		"ne": func(a, b any) (bool, error) { ok, err := eq(a, b); return !ok, err },
		"lt": func(a, b any) (bool, error) { c, err := order(a, b); return c < 0, err },
		"le": func(a, b any) (bool, error) { c, err := order(a, b); return c <= 0, err },
		"gt": func(a, b any) (bool, error) { c, err := order(a, b); return c > 0, err },
		"ge": func(a, b any) (bool, error) { c, err := order(a, b); return c >= 0, err },
	}
	maps.Copy(funcs, documentFuncs(RenderOptions{}))
	return funcs
}

// dict собирает словарь из пар ключ-значение: так в именованный блок передают несколько
// значений, например язык и данные — {{template "block" (dict "lang" "kk" "doc" .)}}.
func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments")
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %v is not a string", pairs[i])
		}
		m[key] = pairs[i+1]
	}
	return m, nil
}

// eq как встроенный: истина, если a равно хотя бы одному из others. Числа сравниваются
// по значению (6000000.00 == 6000000), значения разных типов просто не равны.
func eq(a any, others ...any) (bool, error) {
	if len(others) == 0 {
		return false, fmt.Errorf("eq: missing argument for comparison")
	}
	for _, b := range others {
		if isNumber(a) && isNumber(b) {
			c, err := format.Compare(a, b)
			if err != nil {
				return false, err
			}
			if c == 0 {
				return true, nil
			}
			continue
		}
		switch x := a.(type) {
		case nil:
			if b == nil {
				return true, nil
			}
		case string:
			if y, ok := b.(string); ok && x == y {
				return true, nil
			}
		case bool:
			if y, ok := b.(bool); ok && x == y {
				return true, nil
			}
		}
	}
	return false, nil
}

// order сравнивает два числа или две строки.
func order(a, b any) (int, error) {
	if isNumber(a) && isNumber(b) {
		return format.Compare(a, b)
	}
	x, okA := a.(string)
	y, okB := b.(string)
	if okA && okB {
		return strings.Compare(x, y), nil
	}
	return 0, fmt.Errorf("cannot compare %T and %T", a, b)
}

func isNumber(v any) bool {
	switch v.(type) {
	case json.Number, int, int64, float64:
		return true
	}
	return false
}
