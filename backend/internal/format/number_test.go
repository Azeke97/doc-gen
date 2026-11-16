package format

import (
	"encoding/json"
	"strings"
	"testing"
)

// sp делает тесты читаемыми: в ожиданиях обычный пробел, в результате — неразрывный.
func sp(s string) string {
	return strings.ReplaceAll(s, " ", groupSep)
}

func TestMoney(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{in: json.Number("6000000.00"), want: "6 000 000.00"},
		{in: json.Number("150000"), want: "150 000.00"},
		{in: json.Number("999"), want: "999.00"},
		{in: json.Number("1000"), want: "1 000.00"},
		{in: json.Number("-1234567.5"), want: "-1 234 567.50"},
		{in: json.Number("2.675"), want: "2.68"},
		{in: json.Number("6e6"), want: "6 000 000.00"},
		{in: "6000000.00", want: "6 000 000.00"},
		{in: json.Number("0"), want: "0.00"},
		{in: nil, want: ""},
	}

	for _, tt := range tests {
		got, err := Money(tt.in)
		if err != nil {
			t.Errorf("Money(%v): %v", tt.in, err)
			continue
		}
		if got != sp(tt.want) {
			t.Errorf("Money(%v) = %q, want %q", tt.in, got, sp(tt.want))
		}
	}
}

func TestNumber(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{in: json.Number("1500"), want: "1 500"},
		{in: json.Number("2.50"), want: "2.5"},
		{in: json.Number("1234567.125"), want: "1 234 567.125"},
		{in: json.Number("-0.5"), want: "-0.5"},
		{in: 7, want: "7"},
		{in: nil, want: ""},
	}

	for _, tt := range tests {
		got, err := Number(tt.in)
		if err != nil {
			t.Errorf("Number(%v): %v", tt.in, err)
			continue
		}
		if got != sp(tt.want) {
			t.Errorf("Number(%v) = %q, want %q", tt.in, got, sp(tt.want))
		}
	}
}

func TestNotANumber(t *testing.T) {
	for _, in := range []any{"abc", "6 000", true, []any{1}} {
		if _, err := Money(in); err == nil {
			t.Errorf("Money(%v): want error", in)
		}
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b any
		want int
	}{
		{a: json.Number("6000000.00"), b: 0, want: 1},
		{a: json.Number("0.1"), b: 0.1, want: 0},
		{a: "10", b: json.Number("9"), want: 1},
		{a: json.Number("-1"), b: json.Number("0"), want: -1},
	}

	for _, tt := range tests {
		got, err := Compare(tt.a, tt.b)
		if err != nil || got != tt.want {
			t.Errorf("Compare(%v, %v) = %d, %v; want %d", tt.a, tt.b, got, err, tt.want)
		}
	}

	if _, err := Compare(nil, 1); err == nil {
		t.Error("Compare(nil, 1): want error")
	}
}
