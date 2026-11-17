package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

// readForm читает multipart/form-data потоком: каждая часть — не больше своего предела,
// неизвестные и повторённые части — ошибка. Файлы и поля читаются одинаково, поэтому
// данные можно прислать и файлом, и текстовым полем.
func readForm(w http.ResponseWriter, r *http.Request, limits map[string]int64) (map[string][]byte, *apiError) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return nil, &apiError{status: http.StatusBadRequest, code: "invalid_form", message: "request body must be multipart/form-data"}
	}

	var total int64
	for _, l := range limits {
		total += l
	}
	r.Body = http.MaxBytesReader(w, r.Body, total+1<<20) // запас на заголовки частей

	mr, err := r.MultipartReader()
	if err != nil {
		return nil, &apiError{status: http.StatusBadRequest, code: "invalid_form", message: "cannot read multipart body: " + err.Error()}
	}

	form := make(map[string][]byte, len(limits))
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return form, nil
		}
		if err != nil {
			return nil, formReadError(err)
		}

		name := part.FormName()
		limit, known := limits[name]
		switch {
		case !known:
			_ = part.Close()
			return nil, &apiError{status: http.StatusBadRequest, code: "invalid_form", message: fmt.Sprintf("unknown form field %q", name)}
		case form[name] != nil:
			_ = part.Close()
			return nil, &apiError{status: http.StatusBadRequest, code: "invalid_form", message: fmt.Sprintf("form field %q is repeated", name)}
		}

		b, err := io.ReadAll(io.LimitReader(part, limit+1))
		_ = part.Close()
		if err != nil {
			return nil, formReadError(err)
		}
		if int64(len(b)) > limit {
			return nil, &apiError{status: http.StatusRequestEntityTooLarge, code: "request_too_large", message: fmt.Sprintf("form field %q must not exceed %d bytes", name, limit)}
		}
		form[name] = b
	}
}

func formReadError(err error) *apiError {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return &apiError{status: http.StatusRequestEntityTooLarge, code: "request_too_large", message: "request body is too large"}
	}
	return &apiError{status: http.StatusBadRequest, code: "invalid_form", message: "cannot read multipart body: " + err.Error()}
}

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

// jsonDetails — подробности ошибки для журнала; nil, если их нет.
func jsonDetails(details any) json.RawMessage {
	if details == nil {
		return nil
	}
	b, err := json.Marshal(details)
	if err != nil {
		return nil
	}
	return b
}
