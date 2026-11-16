package format

import (
	"encoding/json"
	"testing"
)

func TestWordsRu(t *testing.T) {
	tests := []struct {
		n    string
		want string
	}{
		{n: "0", want: "ноль"},
		{n: "1", want: "один"},
		{n: "2", want: "два"},
		{n: "11", want: "одиннадцать"},
		{n: "21", want: "двадцать один"},
		{n: "101", want: "сто один"},
		{n: "215", want: "двести пятнадцать"},
		{n: "1000", want: "одна тысяча"},
		{n: "2000", want: "две тысячи"},
		{n: "5000", want: "пять тысяч"},
		{n: "11000", want: "одиннадцать тысяч"},
		{n: "21000", want: "двадцать одна тысяча"},
		{n: "112000", want: "сто двенадцать тысяч"},
		{n: "1000000", want: "один миллион"},
		{n: "2001002", want: "два миллиона одна тысяча два"},
		{n: "6000000", want: "шесть миллионов"},
		{n: "1234567891234", want: "один триллион двести тридцать четыре миллиарда пятьсот шестьдесят семь миллионов восемьсот девяносто одна тысяча двести тридцать четыре"},
		{n: "-5", want: "минус пять"},
	}

	for _, tt := range tests {
		got, err := Words(json.Number(tt.n), "ru")
		if err != nil || got != tt.want {
			t.Errorf("Words(%s, ru) = %q, %v; want %q", tt.n, got, err, tt.want)
		}
	}
}

func TestWordsKk(t *testing.T) {
	tests := []struct {
		n    string
		want string
	}{
		{n: "0", want: "нөл"},
		{n: "1", want: "бір"},
		{n: "10", want: "он"},
		{n: "11", want: "он бір"},
		{n: "21", want: "жиырма бір"},
		{n: "100", want: "жүз"},
		{n: "150", want: "жүз елу"},
		{n: "250", want: "екі жүз елу"},
		{n: "1000", want: "бір мың"},
		{n: "2500", want: "екі мың бес жүз"},
		{n: "150000", want: "жүз елу мың"},
		{n: "1000000", want: "бір миллион"},
		{n: "6000000", want: "алты миллион"},
		{n: "2001002", want: "екі миллион бір мың екі"},
		{n: "94", want: "тоқсан төрт"},
	}

	for _, tt := range tests {
		got, err := Words(json.Number(tt.n), "kk")
		if err != nil || got != tt.want {
			t.Errorf("Words(%s, kk) = %q, %v; want %q", tt.n, got, err, tt.want)
		}
	}
}

func TestMoneyWords(t *testing.T) {
	tests := []struct {
		in     any
		locale string
		want   string
	}{
		{in: json.Number("6000000.00"), locale: "ru", want: "шесть миллионов тенге 00 тиын"},
		{in: json.Number("6000000.00"), locale: "kk", want: "алты миллион теңге 00 тиын"},
		{in: "150000.5", locale: "ru", want: "сто пятьдесят тысяч тенге 50 тиын"},
		{in: json.Number("1001.07"), locale: "kz", want: "бір мың бір теңге 07 тиын"},
		{in: json.Number("2.675"), locale: "ru", want: "два тенге 68 тиын"},
		{in: json.Number("0.99"), locale: "ru", want: "ноль тенге 99 тиын"},
		{in: json.Number("-21000"), locale: "ru", want: "минус двадцать одна тысяча тенге 00 тиын"},
		{in: nil, locale: "ru", want: ""},
	}

	for _, tt := range tests {
		got, err := MoneyWords(tt.in, tt.locale)
		if err != nil || got != tt.want {
			t.Errorf("MoneyWords(%v, %s) = %q, %v; want %q", tt.in, tt.locale, got, err, tt.want)
		}
	}
}

func TestWordsErrors(t *testing.T) {
	if _, err := Words(json.Number("2.5"), "ru"); err == nil {
		t.Error("Words(2.5): want error")
	}
	if _, err := Words(json.Number("1000000000000000"), "ru"); err == nil {
		t.Error("Words(10^15): want error")
	}
	if _, err := Words(json.Number("1"), "en"); err == nil {
		t.Error("Words(en): want error")
	}
	if _, err := MoneyWords("много", "ru"); err == nil {
		t.Error("MoneyWords(много): want error")
	}
}

func TestCapFirst(t *testing.T) {
	for in, want := range map[string]string{"шесть миллионов": "Шесть миллионов", "алты": "Алты", "": "", "6": "6"} {
		if got := CapFirst(in); got != want {
			t.Errorf("CapFirst(%q) = %q, want %q", in, got, want)
		}
	}
}
