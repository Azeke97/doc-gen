package format

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

func TestKzID(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "plain", in: "990840001239", want: "990840001239"},
		{name: "spaces and dashes", in: "9908 4000-1239", want: "990840001239"},
		{name: "number lost leading zero", in: json.Number("50101500010"), want: "050101500010"},
		{name: "missing", in: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := KzID(tt.in)
			if err != nil || got != tt.want {
				t.Errorf("KzID(%v) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}

	for _, in := range []any{"990840001238", "12345", true} {
		if _, err := KzID(in); err == nil {
			t.Errorf("KzID(%v): want error", in)
		}
	}
}
