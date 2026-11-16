package htmlengine

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"image/png"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestDocumentFuncs(t *testing.T) {
	got, err := render(t, `{{docid}} {{verifyurl}}`, `{}`)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := testOptions.DocumentID + " " + testOptions.VerifyURL; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

var qrSrc = regexp.MustCompile(`src="data:image/png;base64,([A-Za-z0-9+/=]+)"`)

func TestQR(t *testing.T) {
	// Два QR, как в двуязычном протоколе: в конце казахского и русского блоков.
	got, err := render(t, `<img src="{{qr}}" alt="QR"><img src="{{qr}}" alt="QR">`, `{}`)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// В атрибуте html/template пишет «+» как &#43;; браузер раскодирует его обратно.
	matches := qrSrc.FindAllStringSubmatch(html.UnescapeString(got), -1)
	if len(matches) != 2 || matches[0][1] != matches[1][1] {
		t.Fatalf("want two identical data: PNG images, got %q", got[:min(len(got), 200)])
	}
	raw, err := base64.StdEncoding.DecodeString(matches[0][1])
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	if b := img.Bounds(); b.Dx() != qrSize || b.Dy() != qrSize {
		t.Errorf("qr size = %v, want %dx%d", b, qrSize, qrSize)
	}
}

// Ссылка data: из данных портала не попадает в src: html/template её обезвреживает.
// Доверять можно только QR, который собрал сам сервис.
func TestDataURLFromDataIsBlocked(t *testing.T) {
	got, err := render(t, `<img src="{{.img}}">`, `{"img": "data:image/png;base64,AAAA"}`)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(got, "data:") {
		t.Errorf("data: url from portal data was rendered: %s", got)
	}
}

func TestQRWithoutVerifyURL(t *testing.T) {
	tmpl, err := Parse([]byte(`<img src="{{qr}}">`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := tmpl.Render(json.RawMessage(`{}`), RenderOptions{}); err == nil {
		t.Error("want error when verify url is empty")
	}
}

// Один разобранный шаблон отрисовывает разные документы параллельно — значения не путаются.
func TestRenderConcurrentDocuments(t *testing.T) {
	tmpl, err := Parse([]byte(`{{docid}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := range 50 {
		wg.Go(func() {
			id := fmt.Sprintf("doc-%02d", i)
			out, err := tmpl.Render(json.RawMessage(`{}`), RenderOptions{DocumentID: id, VerifyURL: "https://x/" + id})
			if err != nil || string(out) != id {
				errs <- fmt.Errorf("render %s = %q: %w", id, out, err)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
