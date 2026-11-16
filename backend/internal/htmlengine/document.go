package htmlengine

import (
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"sync"

	"github.com/skip2/go-qrcode"
)

// qrSize — сторона картинки QR-кода в пикселях; размер на странице задаёт шаблон через CSS.
const qrSize = 512

// RenderOptions — значения конкретного документа, известные только в момент выпуска.
type RenderOptions struct {
	DocumentID string // публичный идентификатор, как в колонтитуле
	VerifyURL  string // адрес страницы проверки документа, его кодирует QR
}

// documentFuncs — функции шаблона, которые зависят от документа: {{docid}}, {{verifyurl}}, {{qr}}.
// При разборе шаблона они должны существовать, поэтому Funcs регистрирует заглушки,
// а Render подставляет настоящие в копию шаблона.
func documentFuncs(opts RenderOptions) template.FuncMap {
	var (
		once sync.Once
		qr   template.URL
		err  error
	)
	return template.FuncMap{
		"docid":     func() string { return opts.DocumentID },
		"verifyurl": func() string { return opts.VerifyURL },
		"qr": func() (template.URL, error) {
			once.Do(func() { qr, err = qrDataURL(opts.VerifyURL) })
			return qr, err
		},
	}
}

// qrDataURL возвращает QR-код ссылки как data:-URL с PNG: документ самодостаточен,
// Gotenberg не ходит в сеть. Тип template.URL говорит html/template, что ссылке можно доверять.
func qrDataURL(link string) (template.URL, error) {
	if link == "" {
		return "", errors.New("qr: verify url is not set")
	}
	png, err := qrcode.Encode(link, qrcode.Medium, qrSize)
	if err != nil {
		return "", fmt.Errorf("qr: %w", err)
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png)), nil //nolint:gosec // ссылку собирает сервис, а не данные портала
}
