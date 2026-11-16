package dataschema

import (
	"errors"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Форматы для казахстанских идентификаторов: в протоколах и договорах это БИН организаций
// и ИИН физлиц. Опечатка в одной цифре даёт документ с чужим идентификатором, поэтому
// проверяем контрольную цифру ещё при приёме данных: "format": "kz-bin" / "kz-iin".
var kzFormats = []*jsonschema.Format{
	{Name: "kz-iin", Validate: validateKzID},
	{Name: "kz-bin", Validate: validateKzID},
}

// validateKzID проверяет ИИН/БИН: 12 цифр и контрольная (последняя) цифра.
// Не строки пропускаем: тип проверяет "type", а не "format".
func validateKzID(v any) error {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if !ValidKzID(s) {
		return errors.New("must be 12 digits with a valid check digit")
	}
	return nil
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
