package htmlengine

import (
	"strings"
	"testing"
)

func TestHelpers(t *testing.T) {
	tests := []struct {
		name string
		src  string
		data string
		want string
	}{
		{name: "money", src: `{{money .sum}}`, data: `{"sum": 6000000}`, want: "6 000 000.00"},
		{name: "money from string", src: `{{money .sum}}`, data: `{"sum": "150000.5"}`, want: "150 000.50"},
		{name: "money missing", src: `[{{money .sum}}]`, data: `{}`, want: "[]"},
		{name: "number", src: `{{number .qty}}`, data: `{"qty": 1500}`, want: "1 500"},
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
		{name: "len still works", src: `{{if gt (len .lots) 0}}есть{{end}}`, data: `{"lots": [1]}`, want: "есть"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := render(t, tt.src, tt.data)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if want := strings.ReplaceAll(tt.want, " ", "\u00a0"); got != want {
				t.Errorf("got %q, want %q", got, want)
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
