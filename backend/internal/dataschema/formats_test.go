package dataschema

import (
	"encoding/json"
	"testing"
)

// Номера синтетические: контрольная цифра посчитана по алгоритму, реальных людей и организаций за ними нет.
func TestValidKzID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{name: "valid", id: "900101300017", want: true},
		{name: "wrong check digit", id: "900101300016"},
		{name: "second weights", id: "000000000101", want: true}, // первая сумма даёт 10, вторая — 1
		{name: "both sums are 10", id: "000000002810"},           // такой номер не выдаётся
		{name: "11 digits", id: "90010130001"},
		{name: "not a digit", id: "90010130001a"},
		{name: "empty", id: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidKzID(tt.id); got != tt.want {
				t.Errorf("ValidKzID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestKzFormatsInSchema(t *testing.T) {
	s := mustCompile(t, `{"type": "object", "properties": {
		"bin": {"type": "string", "format": "kz-bin"},
		"iin": {"type": "string", "format": "kz-iin"}
	}}`)

	errs, err := s.Validate(json.RawMessage(`{"bin": "900101300017", "iin": "900101300016"}`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(errs) != 1 || errs[0].Field != "/iin" {
		t.Errorf("errors = %+v, want one on /iin", errs)
	}
}
