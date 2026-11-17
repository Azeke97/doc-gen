// Package docx подставляет данные в DOCX-шаблон с метками {{ }}: значения, форматтеры,
// циклы и условия по тексту, абзацам и строкам таблиц.
package docx

import (
	"fmt"
	"strings"
)

// Problem — ошибка в шаблоне: что не так и рядом с каким текстом.
type Problem struct {
	Part    string `json:"part"`    // основной текст, верхний или нижний колонтитул
	Context string `json:"context"` // текст абзаца рядом с меткой
	Message string `json:"message"`
}

// TemplateError — шаблон нельзя использовать; Problems перечисляет все найденные ошибки.
type TemplateError struct {
	Problems []Problem
}

func (e *TemplateError) Error() string {
	msgs := make([]string, 0, len(e.Problems))
	for _, p := range e.Problems {
		msgs = append(msgs, p.Message)
	}
	return "invalid template: " + strings.Join(msgs, "; ")
}

// FieldError — ошибка в данных. Field — путь в данных: "lots[2].name".
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// DataError — данные не подходят шаблону; Fields перечисляет ошибки по полям.
type DataError struct {
	Fields []FieldError
}

func (e *DataError) Error() string {
	msgs := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		msgs = append(msgs, f.Field+": "+f.Message)
	}
	return "data does not match template: " + strings.Join(msgs, "; ")
}

// maxFieldErrors — сколько ошибок данных собирать: в цикле на тысячу строк одна и та же
// ошибка повторилась бы тысячу раз.
const maxFieldErrors = 50

func templateError(part, context, format string, args ...any) *TemplateError {
	return &TemplateError{Problems: []Problem{{Part: part, Context: context, Message: fmt.Sprintf(format, args...)}}}
}
