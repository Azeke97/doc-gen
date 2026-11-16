package dataschema

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// protocolSchema — упрощённая схема протокола: номер, сумма, список лотов.
const protocolSchema = `{
	"type": "object",
	"required": ["number", "lots"],
	"properties": {
		"number": {"type": "string", "minLength": 1},
		"date": {"type": "string", "format": "date"},
		"lots": {
			"type": "array",
			"items": {
				"type": "object",
				"required": ["name", "amount"],
				"properties": {
					"name": {"type": "string"},
					"amount": {"type": "number", "minimum": 0}
				}
			}
		}
	}
}`

func mustCompile(t *testing.T, raw string) *Schema {
	t.Helper()

	s, err := Compile(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return s
}

func TestCompileRejects(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr string
	}{
		{name: "not json", schema: `{`, wantErr: "not valid JSON"},
		{name: "array", schema: `[]`, wantErr: "JSON object"},
		{name: "root not object", schema: `{"type": "string"}`, wantErr: `"type": "object"`},
		{name: "bad keyword value", schema: `{"type": "object", "properties": {"a": {"type": "strin"}}}`, wantErr: "invalid schema"},
		{name: "file ref", schema: `{"type": "object", "properties": {"a": {"$ref": "file:///etc/passwd"}}}`, wantErr: "not allowed"},
		{name: "http ref", schema: `{"type": "object", "properties": {"a": {"$ref": "http://example.com/s.json"}}}`, wantErr: "not allowed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile(json.RawMessage(tt.schema))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestCompileAllowsLocalRef(t *testing.T) {
	mustCompile(t, `{
		"type": "object",
		"$defs": {"money": {"type": "number", "minimum": 0}},
		"properties": {"total": {"$ref": "#/$defs/money"}}
	}`)
}

func TestValidate(t *testing.T) {
	s := mustCompile(t, protocolSchema)

	tests := []struct {
		name string
		data string
		want []FieldError // Message — подстрока: точный текст пишет библиотека
	}{
		{
			name: "valid",
			data: `{"number": "209092", "date": "2026-10-20", "lots": [{"name": "Бумага", "amount": 6000000}]}`,
		},
		{
			name: "missing fields",
			data: `{"lots": [{"name": "Бумага"}]}`,
			want: []FieldError{
				{Field: "/lots/0/amount", Message: "is required"},
				{Field: "/number", Message: "is required"},
			},
		},
		{
			name: "wrong types and format",
			data: `{"number": "1", "date": "20.10.2026", "lots": [{"name": "Бумага", "amount": "6 000 000.00"}]}`,
			want: []FieldError{
				{Field: "/date", Message: "is not valid date"},
				{Field: "/lots/0/amount", Message: "got string, want number"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Validate(json.RawMessage(tt.data))
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			match := slices.EqualFunc(got, tt.want, func(g, w FieldError) bool {
				return g.Field == w.Field && strings.Contains(g.Message, w.Message)
			})
			if !match {
				t.Errorf("errors = %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestValidateNotJSON(t *testing.T) {
	s := mustCompile(t, protocolSchema)

	if _, err := s.Validate(json.RawMessage(`{`)); err == nil {
		t.Error("want error for invalid JSON")
	}
}

func TestValidateLimitsErrors(t *testing.T) {
	s := mustCompile(t, `{"type": "object", "properties": {"items": {"type": "array", "items": {"type": "number"}}}}`)

	items := make([]string, MaxErrors+10)
	for i := range items {
		items[i] = `"x"`
	}
	got, err := s.Validate(json.RawMessage(`{"items": [` + strings.Join(items, ",") + `]}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(got) != MaxErrors {
		t.Errorf("len(errors) = %d, want %d", len(got), MaxErrors)
	}
}
