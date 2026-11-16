package format

import (
	"testing"
)

func TestDateTime(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "naive is kazakhstan time", in: "2026-10-20 12:00:12", want: "20.10.2026 12:00:12"},
		{name: "utc converted", in: "2026-10-20T07:00:12Z", want: "20.10.2026 12:00:12"},
		{name: "offset converted", in: "2026-10-20T10:00:12+03:00", want: "20.10.2026 12:00:12"},
		{name: "fraction ignored", in: "2026-10-20T07:00:12.123Z", want: "20.10.2026 12:00:12"},
		{name: "before 2024 reform utc+6", in: "2023-10-20T07:00:12Z", want: "20.10.2023 13:00:12"},
		{name: "without seconds", in: "2026-10-20T12:00", want: "20.10.2026 12:00:00"},
		{name: "date only", in: "2026-10-20", want: "20.10.2026 00:00:00"},
		{name: "missing", in: nil, want: ""},
		{name: "empty", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DateTime(tt.in)
			if err != nil || got != tt.want {
				t.Errorf("DateTime(%v) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestDate(t *testing.T) {
	tests := []struct {
		name   string
		in     any
		layout []string
		want   string
	}{
		{name: "default", in: "2026-10-20", want: "20.10.2026"},
		{name: "utc evening is next day here", in: "2026-10-19T20:30:00Z", want: "20.10.2026"},
		{name: "custom layout", in: "2026-10-20 12:00:12", layout: []string{"2006-01-02 15:04:05"}, want: "2026-10-20 12:00:12"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Date(tt.in, tt.layout...)
			if err != nil || got != tt.want {
				t.Errorf("Date(%v) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestLongDate(t *testing.T) {
	tests := []struct {
		in, locale, want string
	}{
		{in: "2026-10-20", locale: "ru", want: "20 октября 2026 г."},
		{in: "2026-10-20", locale: "kk", want: "2026 жылғы 20 қазан"},
		{in: "2026-03-01", locale: "kz", want: "2026 жылғы 1 наурыз"},
		{in: "2026-12-31T22:00:00Z", locale: "ru", want: "1 января 2027 г."},
	}

	for _, tt := range tests {
		got, err := LongDate(tt.in, tt.locale)
		if err != nil || got != tt.want {
			t.Errorf("LongDate(%q, %q) = %q, %v; want %q", tt.in, tt.locale, got, err, tt.want)
		}
	}
}

func TestDateErrors(t *testing.T) {
	if _, err := Date("20.10.2026"); err == nil {
		t.Error("Date(20.10.2026): want error, input must be ISO")
	}
	if _, err := Date(20261020); err == nil {
		t.Error("Date(number): want error")
	}
	if _, err := Date("2026-10-20", "a", "b"); err == nil {
		t.Error("Date with two layouts: want error")
	}
	if _, err := LongDate("2026-10-20", "en"); err == nil {
		t.Error("LongDate(en): want error")
	}
}
