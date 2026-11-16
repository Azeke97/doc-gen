// Package dataschema — JSON Schema данных шаблона (ТЗ, Ф3): проверка самой схемы,
// проверка данных с перечнем ошибок по полям и сравнение схем двух версий (Ф2).
package dataschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// schemaURL — условный адрес, под которым схема регистрируется в компиляторе.
// Наружу он не уходит; нужен библиотеке, чтобы разрешать внутренние $ref.
const schemaURL = "docgen://schema.json"

// MaxErrors — сколько ошибок по полям отдаём максимум: длинный список никто не читает,
// а невалидные данные с огромным массивом дали бы мегабайтный ответ.
const MaxErrors = 50

var printer = message.NewPrinter(language.English)

// Schema — скомпилированная схема, готовая проверять данные.
type Schema struct {
	compiled *jsonschema.Schema
}

// FieldError — одна ошибка в данных. Field — JSON Pointer до поля ("/lots/0/amount"),
// пустая строка — корень документа.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Compile проверяет, что raw — корректная JSON Schema для данных документа, и компилирует её.
// Схема пишется по draft 2020-12 (если в ней не указан другой $schema), корень — объект.
func Compile(raw json.RawMessage) (*Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("schema is not valid JSON: %w", err)
	}

	root, ok := doc.(map[string]any)
	if !ok {
		return nil, errors.New("schema must be a JSON object")
	}
	if root["type"] != "object" {
		return nil, errors.New(`schema root must have "type": "object": document data is always an object`)
	}

	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat() // "format": "date", "email" и т.п. проверяются, а не просто подсказывают
	c.UseLoader(denyLoader{})

	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}
	compiled, err := c.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}

	return &Schema{compiled: compiled}, nil
}

// Validate проверяет данные и возвращает ошибки по полям; nil — данные подходят.
// err — только если data вообще не JSON.
func (s *Schema) Validate(data json.RawMessage) ([]FieldError, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("data is not valid JSON: %w", err)
	}

	err = s.compiled.Validate(doc)
	if err == nil {
		return nil, nil
	}

	var vErr *jsonschema.ValidationError
	if !errors.As(err, &vErr) {
		return nil, fmt.Errorf("validate: %w", err)
	}

	var out []FieldError
	collect(vErr, &out)

	// Порядок обхода библиотеки не гарантирован — сортируем, чтобы ответ был стабильным.
	slices.SortFunc(out, func(a, b FieldError) int {
		return strings.Compare(a.Field+"\x00"+a.Message, b.Field+"\x00"+b.Message)
	})
	out = slices.Compact(out)
	if len(out) > MaxErrors {
		out = out[:MaxErrors]
	}

	return out, nil
}

// collect собирает «листья» дерева ошибок: именно они говорят, что не так с конкретным полем.
func collect(e *jsonschema.ValidationError, out *[]FieldError) {
	if len(e.Causes) > 0 {
		for _, cause := range e.Causes {
			collect(cause, out)
		}
		return
	}

	field := pointer(e.InstanceLocation)

	// Для отсутствующих полей указываем само поле, а не объект, в котором его нет.
	if req, ok := e.ErrorKind.(*kind.Required); ok {
		for _, name := range req.Missing {
			*out = append(*out, FieldError{Field: field + "/" + escape(name), Message: "is required"})
		}
		return
	}

	*out = append(*out, FieldError{Field: field, Message: e.ErrorKind.LocalizedString(printer)})
}

// pointer собирает JSON Pointer (RFC 6901) из пути до значения.
func pointer(tokens []string) string {
	var sb strings.Builder
	for _, t := range tokens {
		sb.WriteByte('/')
		sb.WriteString(escape(t))
	}
	return sb.String()
}

func escape(token string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(token)
}

// denyLoader запрещает схеме ссылаться на внешние документы. Без него библиотека
// по умолчанию читает $ref вида file:///etc/passwd с диска сервера.
type denyLoader struct{}

func (denyLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external $ref %q is not allowed: keep all definitions inside the schema ($defs)", url)
}
