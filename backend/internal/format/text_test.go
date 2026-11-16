package format

import "testing"

func TestCheck(t *testing.T) {
	for in, want := range map[any]string{true: "☒", false: "☐", nil: "☐"} {
		if got, err := Check(in); err != nil || got != want {
			t.Errorf("Check(%v) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Check("да"); err == nil {
		t.Error(`Check("да"): want error`)
	}
}

func TestTr(t *testing.T) {
	name := map[string]any{"ru": "Бумага офисная", "kk": "Кеңсе қағазы"}

	tests := []struct {
		name   string
		in     any
		locale string
		want   string
	}{
		{name: "ru", in: name, locale: "ru", want: "Бумага офисная"},
		{name: "kk", in: name, locale: "kk", want: "Кеңсе қағазы"},
		{name: "legacy kz", in: name, locale: "kz", want: "Кеңсе қағазы"},
		{name: "same in all languages", in: "ТОО «Альфа»", locale: "kk", want: "ТОО «Альфа»"},
		{name: "missing", in: nil, locale: "ru", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Tr(tt.in, tt.locale)
			if err != nil || got != tt.want {
				t.Errorf("Tr(%v, %s) = %q, %v; want %q", tt.in, tt.locale, got, err, tt.want)
			}
		})
	}

	errCases := []any{map[string]any{"ru": "только рус"}, map[string]any{"kk": 5}, []any{"x"}}
	for _, in := range errCases {
		if _, err := Tr(in, "kk"); err == nil {
			t.Errorf("Tr(%v, kk): want error", in)
		}
	}
}
