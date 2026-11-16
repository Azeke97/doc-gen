// Package htmlengine — HTML-движок шаблонов (ТЗ, Ф5): html/template + данные JSON → HTML,
// который потом превращается в PDF через Gotenberg.
package htmlengine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
)

// MaxOutput — предельный размер готового HTML.
const MaxOutput = 20 << 20 // 20 МиБ

// ErrRender — шаблон не отрисовался на этих данных: виноваты шаблон или данные, а не сервис.
var ErrRender = errors.New("render template")

var errOutputTooLarge = fmt.Errorf("output exceeds %d bytes", MaxOutput)

// Template — разобранный шаблон. Можно отрисовывать из нескольких горутин одновременно.
type Template struct {
	tmpl      *template.Template
	maxOutput int
}

func Parse(src []byte) (*Template, error) {
	t, err := template.New("document").Funcs(Funcs()).Parse(string(src))
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	return &Template{
		tmpl:      t,
		maxOutput: MaxOutput,
	}, nil
}

// Render отрисовывает шаблон на данных. Числа остаются json.Number, чтобы суммы
// выводились ровно так, как их прислали, без округления float64.
func (t *Template) Render(data json.RawMessage) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var v map[string]any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: data must be a JSON object: %w", ErrRender, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: data must contain a single JSON object", ErrRender)
	}

	var buf bytes.Buffer
	if err := t.tmpl.Execute(&limitWriter{w: &buf, left: t.maxOutput}, v); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRender, err)
	}
	return buf.Bytes(), nil
}

// limitWriter прерывает запись, когда превышен лимит байт.
type limitWriter struct {
	w    io.Writer
	left int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if len(p) > l.left {
		return 0, errOutputTooLarge
	}
	l.left -= len(p)
	return l.w.Write(p)
}
