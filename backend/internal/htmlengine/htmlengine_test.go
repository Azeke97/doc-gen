package htmlengine

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func render(t *testing.T, src, data string) (string, error) {
	t.Helper()

	tmpl, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := tmpl.Render(json.RawMessage(data))
	return string(out), err
}

func TestRender(t *testing.T) {
	tests := []struct {
		name string
		src  string
		data string
		want string
	}{
		{
			name: "escapes data",
			src:  `<p>{{.name}}</p>`,
			data: `{"name": "ТОО <script>alert(1)</script>"}`,
			want: `<p>ТОО &lt;script&gt;alert(1)&lt;/script&gt;</p>`,
		},
		{
			name: "money kept as sent",
			src:  `{{.amount}}`,
			data: `{"amount": 6000000.00}`,
			want: `6000000.00`,
		},
		{
			name: "missing optional field is empty",
			src:  `[{{.note}}]{{if .note}}есть{{else}}нет{{end}}`,
			data: `{}`,
			want: `[]нет`,
		},
		{
			name: "empty list row",
			src:  `{{range .lots}}<tr><td>{{.name}}</td></tr>{{else}}<tr><td>Нет данных</td></tr>{{end}}`,
			data: `{"lots": []}`,
			want: `<tr><td>Нет данных</td></tr>`,
		},
		{
			name: "nested loops",
			src:  `{{range .applicants}}{{.name}}:{{range .docs}}{{if .ok}}☒{{else}}☐{{end}}{{end}};{{end}}`,
			data: `{"applicants": [{"name": "А", "docs": [{"ok": true}, {"ok": false}]}, {"name": "Б", "docs": []}]}`,
			want: `А:☒☐;Б:;`,
		},
		{
			name: "root data inside loop",
			src:  `{{range .lots}}{{$.number}}-{{.n}} {{end}}`,
			data: `{"number": "209092", "lots": [{"n": 1}, {"n": 2}]}`,
			want: `209092-1 209092-2 `,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := render(t, tt.src, tt.data)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestParseError(t *testing.T) {
	for _, src := range []string{`{{if .a}}`, `{{money .a}}`} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("Parse(%q): want error", src)
		}
	}
}

func TestRenderErrors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		data    string
		wantErr string
	}{
		{name: "not an object", src: `x`, data: `[1]`, wantErr: "JSON object"},
		{name: "two values", src: `x`, data: `{} {}`, wantErr: "single JSON object"},
		{name: "range over string", src: `{{range .title}}{{end}}`, data: `{"title": "x"}`, wantErr: "range can't iterate"},
		{name: "compare json number", src: `{{if gt .sum 0}}{{end}}`, data: `{"sum": 1}`, wantErr: "incompatible types"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := render(t, tt.src, tt.data)
			if !errors.Is(err, ErrRender) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want ErrRender containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRenderOutputLimit(t *testing.T) {
	tmpl, err := Parse([]byte(`{{range .rows}}<tr><td>{{.}}</td></tr>{{end}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	tmpl.maxOutput = 30

	_, err = tmpl.Render(json.RawMessage(`{"rows": [1, 2, 3, 4, 5]}`))
	if !errors.Is(err, ErrRender) || !errors.Is(err, errOutputTooLarge) {
		t.Errorf("err = %v, want ErrRender wrapping errOutputTooLarge", err)
	}
}
