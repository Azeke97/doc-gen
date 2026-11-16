// Package format форматирует значения из данных документа: деньги, числа, даты, суммы прописью.
// Функции принимают значения так, как их отдаёт JSON (json.Number, string, nil), и годятся для любого движка.
package format

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// groupSep разделяет разряды. Неразрывный пробел не даёт числу переноситься в узкой ячейке таблицы.
const groupSep = "\u00a0"

// maxDecimals — сколько знаков после точки показывает Number.
const maxDecimals = 10

var errNotNumber = errors.New("not a number")

// Decimal переводит значение в точное десятичное число. ok = false для nil (поле не передано).
// Строки допускаются: порталы часто передают суммы строкой, чтобы не терять копейки.
func Decimal(v any) (d *big.Rat, ok bool, err error) {
	var s string
	switch x := v.(type) {
	case nil:
		return nil, false, nil
	case json.Number:
		s = x.String()
	case string:
		s = x
	case int:
		s = strconv.Itoa(x)
	case int64:
		s = strconv.FormatInt(x, 10)
	case float64:
		s = strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return nil, false, fmt.Errorf("%w: %T", errNotNumber, v)
	}

	d, ok = new(big.Rat).SetString(s)
	if !ok {
		return nil, false, fmt.Errorf("%w: %q", errNotNumber, s)
	}
	return d, true, nil
}

// Money форматирует сумму: два знака после точки и разряды через пробел — "6 000 000.00".
// Округление — половина от нуля (2.675 → 2.68). nil — пустая строка.
func Money(v any) (string, error) {
	d, ok, err := Decimal(v)
	if err != nil || !ok {
		return "", err
	}
	return group(d.FloatString(2)), nil
}

// Number форматирует число без лишних нулей: "1 500", "2.5". nil — пустая строка.
func Number(v any) (string, error) {
	d, ok, err := Decimal(v)
	if err != nil || !ok {
		return "", err
	}

	s := d.FloatString(maxDecimals)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return group(s), nil
}

// Compare сравнивает два числа: -1, 0 или 1.
func Compare(a, b any) (int, error) {
	x, okA, err := Decimal(a)
	if err != nil {
		return 0, err
	}
	y, okB, err := Decimal(b)
	if err != nil {
		return 0, err
	}
	if !okA || !okB {
		return 0, fmt.Errorf("%w: nil", errNotNumber)
	}
	return x.Cmp(y), nil
}

// group разбивает целую часть на разряды по три цифры.
func group(s string) string {
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	intPart, frac, hasFrac := strings.Cut(s, ".")

	var b strings.Builder
	b.WriteString(sign)
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteString(groupSep)
		}
		b.WriteRune(r)
	}
	if hasFrac {
		b.WriteString(".")
		b.WriteString(frac)
	}
	return b.String()
}
