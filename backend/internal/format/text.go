package format

import "fmt"

// Check — отметка в чек-листе: ☒ для true, ☐ для false и отсутствующего значения.
func Check(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "☐", nil
	case bool:
		if x {
			return "☒", nil
		}
		return "☐", nil
	}
	return "", fmt.Errorf("check expects true or false, got %T", v)
}

// Tr выбирает значение на нужном языке. Поле может быть строкой (одинаково на всех языках)
// или объектом {"ru": "...", "kk": "..."}. Если перевода нет — ошибка: документ на двух языках
// не должен молча выйти с пустым местом. nil — пустая строка.
func Tr(v any, locale string) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case map[string]any:
		locale = normalizeLocale(locale)
		value, ok := x[locale]
		if !ok || value == nil {
			return "", fmt.Errorf("no %q value", locale)
		}
		s, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("%q value must be a string, got %T", locale, value)
		}
		return s, nil
	}
	return "", fmt.Errorf("tr expects a string or an object with ru/kk values, got %T", v)
}
