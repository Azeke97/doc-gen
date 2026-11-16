package htmlengine

import (
	"testing"
)

func TestHelpers(t *testing.T) {
	tests := []struct {
		name string
		src  string
		data string
		want string
	}{
		{name: "money", src: `{{money .sum}}`, data: `{"sum": 6000000}`, want: "6\u00a0000\u00a0000.00"},
		{name: "money from string", src: `{{money .sum}}`, data: `{"sum": "150000.5"}`, want: "150\u00a0000.50"},
		{name: "money missing", src: `[{{money .sum}}]`, data: `{}`, want: "[]"},
		{name: "number", src: `{{number .qty}}`, data: `{"qty": 1500}`, want: "1\u00a0500"},
		{name: "gt json number", src: `{{if gt .sum 0}}да{{else}}нет{{end}}`, data: `{"sum": 6000000.00}`, want: "да"},
		{name: "zero is not greater", src: `{{if gt .count 0}}да{{else}}нет{{end}}`, data: `{"count": 0}`, want: "нет"},
		{name: "le float constant", src: `{{if le .rate 0.5}}да{{end}}`, data: `{"rate": 0.5}`, want: "да"},
		{name: "eq numbers by value", src: `{{if eq .a 6000000}}да{{end}}`, data: `{"a": 6000000.00}`, want: "да"},
		{name: "eq strings", src: `{{if eq .status "won"}}победитель{{end}}`, data: `{"status": "won"}`, want: "победитель"},
		{name: "eq any of", src: `{{if eq .status "won" "second"}}да{{end}}`, data: `{"status": "second"}`, want: "да"},
		{name: "eq different types", src: `{{if eq .n "1"}}да{{else}}нет{{end}}`, data: `{"n": 1}`, want: "нет"},
		{name: "eq missing field", src: `{{if eq .status "won"}}да{{else}}нет{{end}}`, data: `{}`, want: "нет"},
		{name: "ne", src: `{{if ne .status "won"}}да{{end}}`, data: `{"status": "lost"}`, want: "да"},
		{name: "lt strings", src: `{{if lt .a .b}}да{{end}}`, data: `{"a": "2026-01-01", "b": "2026-02-01"}`, want: "да"},
		{name: "date", src: `{{date .d}}`, data: `{"d": "2026-10-20"}`, want: "20.10.2026"},
		{name: "datetime pilot layout", src: `{{datetime .t "2006-01-02 15:04:05"}}`, data: `{"t": "2026-10-20T07:00:12Z"}`, want: "2026-10-20 12:00:12"},
		{name: "longdate bilingual", src: `{{longdate .d "kk"}} / {{longdate .d "ru"}}`, data: `{"d": "2026-10-20"}`, want: "2026 жылғы 20 қазан / 20 октября 2026 г."},
		{name: "money in words bilingual", src: `{{money .sum}} ({{capfirst (moneywords .sum "ru")}}) / {{moneywords .sum "kk"}}`, data: `{"sum": "6000000.00"}`, want: "6\u00a0000\u00a0000.00 (Шесть миллионов тенге 00 тиын) / алты миллион теңге 00 тиын"},
		{name: "count in words", src: `{{.n}} ({{words .n "ru"}})`, data: `{"n": 3}`, want: "3 (три)"},
		{name: "bin normalized", src: `{{bin .bin}}`, data: `{"bin": "9908 4000 1239"}`, want: "990840001239"},
		{name: "iin leading zero", src: `{{iin .iin}}`, data: `{"iin": 50101500010}`, want: "050101500010"},
		{
			name: "pilot checklist bilingual",
			src:  `{{range .applicants}}{{tr .name "kk"}} / {{tr .name "ru"}}:{{range .docs}} {{check .ok}}{{end}};{{end}}`,
			data: `{"applicants": [{"name": {"ru": "ТОО «Альфа»", "kk": "«Альфа» ЖШС"}, "docs": [{"ok": true}, {"ok": false}, {}]}]}`,
			want: "«Альфа» ЖШС / ТОО «Альфа»: ☒ ☐ ☐;",
		},
		{name: "dict into named block", src: `{{define "b"}}{{.lang}}:{{.doc.n}}{{end}}{{template "b" (dict "lang" "kk" "doc" .)}}`, data: `{"n": 1}`, want: "kk:1"},
		{name: "inc for numbering", src: `{{range $i, $x := .xs}}{{inc $i}}{{end}}`, data: `{"xs": ["a", "b"]}`, want: "12"},
		{name: "len still works", src: `{{if gt (len .lots) 0}}есть{{end}}`, data: `{"lots": [1]}`, want: "есть"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := render(t, tt.src, tt.data)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHelperErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		data string
	}{
		{name: "money not a number", src: `{{money .sum}}`, data: `{"sum": "много"}`},
		{name: "compare number and string", src: `{{if gt .sum "0"}}{{end}}`, data: `{"sum": 1}`},
		{name: "date in wrong format", src: `{{date .d}}`, data: `{"d": "20.10.2026"}`},
		{name: "words of fraction", src: `{{words .n "ru"}}`, data: `{"n": 2.5}`},
		{name: "invalid bin", src: `{{bin .bin}}`, data: `{"bin": "990840001238"}`},
		{name: "missing translation", src: `{{tr .name "kk"}}`, data: `{"name": {"ru": "только рус"}}`},
		{name: "dict odd args", src: `{{dict "a"}}`, data: `{}`},
		{name: "compare missing", src: `{{if gt .sum 0}}{{end}}`, data: `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := render(t, tt.src, tt.data); err == nil {
				t.Error("want error")
			}
		})
	}
}
