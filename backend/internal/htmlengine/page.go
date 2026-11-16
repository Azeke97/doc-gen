package htmlengine

import (
	"bytes"
	"fmt"
	"html"
)

// PreviewWatermark — водяной знак предпросмотра (ru · kk).
const PreviewWatermark = "ОБРАЗЕЦ · ҮЛГІ"

// Footer возвращает колонтитул документа: идентификатор слева, «страница / всего» справа.
// Номера страниц подставляет Chromium в элементы с классами pageNumber и totalPages.
func Footer(docID string) []byte {
	return fmt.Appendf(nil, `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>
  .footer { box-sizing: border-box; width: 100%%; padding: 0 15mm; display: flex; justify-content: space-between;
            font: 8pt "Liberation Sans", sans-serif; color: #444; }
</style>
</head>
<body><div class="footer"><span>%s</span><span><span class="pageNumber"></span> / <span class="totalPages"></span></span></div></body>
</html>`, html.EscapeString(docID))
}

// WithWatermark добавляет диагональную надпись на каждую страницу. Элемент с position: fixed
// Chromium повторяет на всех печатных страницах; all: initial не даёт стилям шаблона его изменить.
func WithWatermark(page []byte, text string) []byte {
	mark := fmt.Appendf(nil, `<div style="all: initial; position: fixed; top: 50%%; left: 50%%; `+
		`transform: translate(-50%%, -50%%) rotate(-30deg); z-index: 2147483647; white-space: nowrap; `+
		`font: bold 96px 'Liberation Sans', sans-serif; color: rgba(200, 0, 0, 0.15);">%s</div>`, html.EscapeString(text))

	i := bytes.LastIndex(bytes.ToLower(page), []byte("</body>"))
	if i < 0 {
		return append(append([]byte{}, page...), mark...)
	}

	out := make([]byte, 0, len(page)+len(mark))
	out = append(out, page[:i]...)
	out = append(out, mark...)
	return append(out, page[i:]...)
}
