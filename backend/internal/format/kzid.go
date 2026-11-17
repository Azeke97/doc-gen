package format

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var errNotKzID = errors.New("неверный БИН/ИИН")

// KzID нормализует БИН/ИИН: убирает пробелы и дефисы и проверяет контрольную цифру.
// Число дополняется нулями слева: ИИН родившихся в 2000-х начинается с 0, и в JSON-числе
// этот ноль теряется. nil — пустая строка.
func KzID(v any) (string, error) {
	var s string
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		s = strings.NewReplacer(" ", "", "-", "", groupSep, "").Replace(x)
	case json.Number:
		s = x.String()
		if len(s) < 12 {
			s = strings.Repeat("0", 12-len(s)) + s
		}
	default:
		return "", fmt.Errorf("%w: ожидается строка или число, получено %s", errNotKzID, kind(v))
	}

	if !ValidKzID(s) {
		return "", fmt.Errorf("%w: %q", errNotKzID, s)
	}
	return s, nil
}

// ValidKzID — 12 цифр, последняя совпадает с контрольной суммой по алгоритму ИИН/БИН:
// сумма цифр с весами 1..11 по модулю 11; если вышло 10 — веса 3..11,1,2;
// если снова 10 — номер недействителен.
func ValidKzID(s string) bool {
	if len(s) != 12 {
		return false
	}
	var d [12]int
	for i := range 12 {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
		d[i] = int(s[i] - '0')
	}

	check := checksum(d, [11]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11})
	if check == 10 {
		check = checksum(d, [11]int{3, 4, 5, 6, 7, 8, 9, 10, 11, 1, 2})
	}

	return check != 10 && check == d[11]
}

func checksum(d [12]int, weights [11]int) int {
	sum := 0
	for i, w := range weights {
		sum += d[i] * w
	}
	return sum % 11
}
