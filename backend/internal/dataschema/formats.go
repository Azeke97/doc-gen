package dataschema

import (
	"errors"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"docgen/internal/format"
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
	if !format.ValidKzID(s) {
		return errors.New("must be 12 digits with a valid check digit")
	}
	return nil
}
