package format

import (
	"errors"
	"fmt"
	"strconv"
	"time"
	_ "time/tzdata" // база часовых поясов внутри бинарника: в контейнере её может не быть
)

// Kazakhstan — часовой пояс документов. Именно зона, а не «+05:00»: до 1 марта 2024 года
// Алматы жил по UTC+6, и старые даты должны выводиться по тогдашнему времени.
var Kazakhstan = mustLoadLocation("Asia/Almaty")

const (
	dateLayout     = "02.01.2006"
	dateTimeLayout = "02.01.2006 15:04:05"
)

// inputLayouts — в каком виде порталы присылают даты. Время без смещения считается временем Казахстана.
var inputLayouts = []struct {
	layout   string
	dateOnly bool
}{
	{layout: "2006-01-02", dateOnly: true},
	{layout: time.RFC3339},
	{layout: "2006-01-02T15:04:05"},
	{layout: "2006-01-02 15:04:05"},
	{layout: "2006-01-02T15:04"},
	{layout: "2006-01-02 15:04"},
}

var errNotDate = errors.New("не дата")

var (
	monthsRu = [12]string{"января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря"}
	monthsKk = [12]string{"қаңтар", "ақпан", "наурыз", "сәуір", "мамыр", "маусым",
		"шілде", "тамыз", "қыркүйек", "қазан", "қараша", "желтоқсан"}
)

// Time разбирает дату или дату со временем и переводит её во время Казахстана.
// ok = false для nil и пустой строки.
func Time(v any) (t time.Time, ok bool, err error) {
	var s string
	switch x := v.(type) {
	case nil:
		return time.Time{}, false, nil
	case string:
		s = x
	default:
		return time.Time{}, false, fmt.Errorf("%w: ожидается строка, получено %s", errNotDate, kind(v))
	}
	if s == "" {
		return time.Time{}, false, nil
	}

	for _, in := range inputLayouts {
		t, err := time.ParseInLocation(in.layout, s, Kazakhstan)
		if err != nil {
			continue
		}
		if in.dateOnly {
			return t, true, nil
		}
		return t.In(Kazakhstan), true, nil
	}
	return time.Time{}, false, fmt.Errorf("%w: %q (нужно ГГГГ-ММ-ДД, ГГГГ-ММ-ДД ЧЧ:ММ[:СС] или RFC 3339)", errNotDate, s)
}

// Date — "20.10.2026". Второй аргумент — свой формат в нотации Go, например "2006-01-02".
func Date(v any, layout ...string) (string, error) {
	return formatTime(v, dateLayout, layout)
}

// DateTime — "20.10.2026 12:00:12". Второй аргумент — свой формат в нотации Go.
func DateTime(v any, layout ...string) (string, error) {
	return formatTime(v, dateTimeLayout, layout)
}

// LongDate — дата словами: "20 октября 2026 г." (ru) или "2026 жылғы 20 қазан" (kk).
func LongDate(v any, locale string) (string, error) {
	t, ok, err := Time(v)
	if err != nil || !ok {
		return "", err
	}

	day, month, year := strconv.Itoa(t.Day()), t.Month()-1, strconv.Itoa(t.Year())
	switch normalizeLocale(locale) {
	case "ru":
		return day + " " + monthsRu[month] + " " + year + " г.", nil
	case "kk":
		return year + " жылғы " + day + " " + monthsKk[month], nil
	}
	return "", fmt.Errorf("язык %q не поддерживается: нужен ru или kk", locale)
}

func formatTime(v any, defaultLayout string, layout []string) (string, error) {
	if len(layout) > 1 {
		return "", errors.New("можно указать только один формат даты")
	}
	t, ok, err := Time(v)
	if err != nil || !ok {
		return "", err
	}
	if len(layout) == 1 {
		return t.Format(layout[0]), nil
	}
	return t.Format(defaultLayout), nil
}

// normalizeLocale принимает устаревший код kz как kk.
func normalizeLocale(locale string) string {
	if locale == "kz" {
		return "kk"
	}
	return locale
}

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}
