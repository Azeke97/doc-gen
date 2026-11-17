// Package gotenberg превращает DOCX (LibreOffice) и HTML (Chromium) в PDF через Gotenberg.
package gotenberg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// MaxPDF — предельный размер PDF в ответе Gotenberg.
const MaxPDF = 100 << 20 // 100 МиБ

var (
	// ErrConvert — Gotenberg отклонил документ (битый файл, отсутствующий ресурс):
	// это ошибка шаблона, а не сбой сервиса.
	ErrConvert = errors.New("convert to pdf")
	// ErrUnavailable — Gotenberg недоступен, перегружен или не успел: запрос можно повторить позже.
	ErrUnavailable = errors.New("pdf converter is unavailable")
)

// DefaultConcurrency — сколько конвертаций одновременно уходит в Gotenberg, если не задано иное.
const DefaultConcurrency = 4

// Имена файлов, которые Gotenberg понимает особым образом.
const (
	indexFile  = "index.html"
	headerFile = "header.html"
	footerFile = "footer.html"
)

// Document — то, что нужно превратить в PDF.
type Document struct {
	HTML   []byte
	Header []byte            // необязательно; повторяется на каждой странице
	Footer []byte            // необязательно; .pageNumber и .totalPages заполняются номерами страниц
	Assets map[string][]byte // картинки и шрифты, на которые HTML ссылается по имени файла
}

// Client — клиент Gotenberg.
type Client struct {
	baseURL string
	http    *http.Client
	maxPDF  int64
	slots   chan struct{} // семафор: занятый слот — конвертация в работе
}

// New создаёт клиент. timeout ограничивает одну конвертацию целиком, включая ожидание
// свободного слота; одновременно идёт не больше DefaultConcurrency конвертаций.
func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: timeout,
		},
		maxPDF: MaxPDF,
		slots:  make(chan struct{}, DefaultConcurrency),
	}
}

// WithConcurrency задаёт, сколько конвертаций одновременно отправлять в Gotenberg.
// LibreOffice тяжёлый: без предела всплеск запросов съест память контейнера.
func (c *Client) WithConcurrency(n int) *Client {
	c.slots = make(chan struct{}, max(n, 1))
	return c
}

// ConvertDOCX превращает DOCX в PDF через LibreOffice. Размер страницы, поля и колонтитулы
// берутся из самого документа.
func (c *Client) ConvertDOCX(ctx context.Context, docx []byte, trace string) ([]byte, error) {
	if len(docx) == 0 {
		return nil, fmt.Errorf("%w: document is empty", ErrConvert)
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("files", "document.docx")
	if err != nil {
		return nil, fmt.Errorf("add document: %w", err)
	}
	if _, err := part.Write(docx); err != nil {
		return nil, fmt.Errorf("add document: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close form: %w", err)
	}

	return c.convert(ctx, "/forms/libreoffice/convert", &buf, w.FormDataContentType(), trace)
}

// ConvertHTML возвращает PDF. Размер страницы и поля задаёт шаблон через CSS @page,
// без него страница — A4. trace уходит в логи Gotenberg, чтобы связать их с нашим запросом.
func (c *Client) ConvertHTML(ctx context.Context, doc Document, trace string) ([]byte, error) {
	body, contentType, err := buildForm(doc)
	if err != nil {
		return nil, err
	}
	return c.convert(ctx, "/forms/chromium/convert/html", body, contentType, trace)
}

// convert отправляет форму в маршрут Gotenberg и возвращает PDF. Ждёт свободный слот,
// пока не истечёт ctx или таймаут клиента.
func (c *Client) convert(ctx context.Context, route string, body io.Reader, contentType, trace string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.http.Timeout)
	defer cancel()

	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: all %d conversion slots are busy: %w", ErrUnavailable, cap(c.slots), ctx.Err())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+route, body) //nolint:gosec // адрес из конфигурации, не из запроса
	if err != nil {
		return nil, fmt.Errorf("build gotenberg request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	if trace != "" {
		req.Header.Set("Gotenberg-Trace", trace)
	}

	resp, err := c.http.Do(req) //nolint:gosec // адрес из конфигурации, не из запроса
	if err != nil {
		// Нет соединения или вышло время — конвертер недоступен, а не документ плохой.
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		text := strings.TrimSpace(string(msg))
		switch resp.StatusCode {
		case http.StatusBadRequest, http.StatusConflict:
			return nil, fmt.Errorf("%w: %s", ErrConvert, text)
		case http.StatusServiceUnavailable, http.StatusTooManyRequests:
			return nil, fmt.Errorf("%w: gotenberg returned %d: %s", ErrUnavailable, resp.StatusCode, text)
		}
		return nil, fmt.Errorf("gotenberg returned %d: %s", resp.StatusCode, text)
	}

	pdf, err := io.ReadAll(io.LimitReader(resp.Body, c.maxPDF+1))
	if err != nil {
		return nil, fmt.Errorf("read pdf: %w", err)
	}
	if int64(len(pdf)) > c.maxPDF {
		return nil, fmt.Errorf("%w: pdf exceeds %d bytes", ErrConvert, c.maxPDF)
	}

	return pdf, nil
}

// buildForm собирает multipart-форму запроса к Gotenberg.
func buildForm(doc Document) (*bytes.Buffer, string, error) {
	if len(doc.HTML) == 0 {
		return nil, "", fmt.Errorf("%w: html is empty", ErrConvert)
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	files := map[string][]byte{indexFile: doc.HTML}
	if len(doc.Header) > 0 {
		files[headerFile] = doc.Header
	}
	if len(doc.Footer) > 0 {
		files[footerFile] = doc.Footer
	}
	for name, content := range doc.Assets {
		if err := checkAssetName(name); err != nil {
			return nil, "", err
		}
		files[name] = content
	}

	for name, content := range files {
		part, err := w.CreateFormFile("files", name)
		if err != nil {
			return nil, "", fmt.Errorf("add %s: %w", name, err)
		}
		if _, err := part.Write(content); err != nil {
			return nil, "", fmt.Errorf("add %s: %w", name, err)
		}
	}

	fields := map[string]string{
		"paperWidth":                  "210mm",
		"paperHeight":                 "297mm",
		"preferCssPageSize":           "true",
		"printBackground":             "true",
		"failOnResourceLoadingFailed": "true",
	}
	for name, value := range fields {
		if err := w.WriteField(name, value); err != nil {
			return nil, "", fmt.Errorf("add field %s: %w", name, err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("close form: %w", err)
	}

	return &buf, w.FormDataContentType(), nil
}

// checkAssetName не даёт файлу затереть index/header/footer или указать путь.
func checkAssetName(name string) error {
	switch {
	case name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, "."):
		return fmt.Errorf("%w: invalid asset name %q", ErrConvert, name)
	case name == indexFile || name == headerFile || name == footerFile:
		return fmt.Errorf("%w: asset name %q is reserved", ErrConvert, name)
	}
	return nil
}
