package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"docgen/internal/templates"
)

// multipartBody собирает тело multipart/form-data: файл file и текстовые поля.
func multipartBody(t *testing.T, file []byte, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	if file != nil {
		fw, err := mw.CreateFormFile("file", "template.html")
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := fw.Write(file); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	return &buf, mw.FormDataContentType()
}

func (s *adminTestServer) upload(path string, body *bytes.Buffer, contentType, token string) *httptest.ResponseRecorder {
	s.t.Helper()

	req := httptest.NewRequestWithContext(s.t.Context(), http.MethodPost, path, body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	return rec
}

func TestUploadVersion(t *testing.T) {
	path := "/admin/v1/templates/" + uuid.NewString() + "/versions"
	file := []byte("<h1>{{.title}}</h1>")
	schema := `{"type":"object"}`

	tests := []struct {
		name       string
		created    bool
		svcErr     error
		wantStatus int
	}{
		{name: "new draft", created: true, wantStatus: http.StatusCreated},
		{name: "draft replaced", created: false, wantStatus: http.StatusOK},
		{name: "no template", svcErr: templates.ErrTemplateNotFound, wantStatus: http.StatusNotFound},
		{name: "archived", svcErr: templates.ErrTemplateArchived, wantStatus: http.StatusConflict},
		{name: "invalid", svcErr: &templates.ValidationError{Field: "file", Message: "bad"}, wantStatus: http.StatusBadRequest},
		{name: "draft changed", svcErr: templates.ErrDraftChanged, wantStatus: http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newAdminTestServer(t)
			s.templates.created, s.templates.err = tt.created, tt.svcErr
			token := s.login()

			body, ct := multipartBody(t, file, map[string]string{"schema": schema, "test_data": `{"title":"x"}`})
			rec := s.upload(path, body, ct, token)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if !bytes.Equal(s.templates.gotUpload.File, file) || string(s.templates.gotUpload.Schema) != schema {
				t.Errorf("upload params not passed through: %+v", s.templates.gotUpload)
			}
		})
	}
}

func TestUploadVersionBadRequest(t *testing.T) {
	s := newAdminTestServer(t)
	token := s.login()

	t.Run("bad id", func(t *testing.T) {
		body, ct := multipartBody(t, []byte("x"), nil)
		rec := s.upload("/admin/v1/templates/not-a-uuid/versions", body, ct, token)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_id")
	})

	t.Run("no file", func(t *testing.T) {
		body, ct := multipartBody(t, nil, map[string]string{"schema": "{}"})
		rec := s.upload("/admin/v1/templates/"+uuid.NewString()+"/versions", body, ct, token)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_file")
	})

	t.Run("not multipart", func(t *testing.T) {
		rec := s.upload("/admin/v1/templates/"+uuid.NewString()+"/versions",
			bytes.NewBufferString(`{"file":"x"}`), "application/json", token)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_form")
	})
}

func TestUploadVersionExpectedDraft(t *testing.T) {
	path := "/admin/v1/templates/" + uuid.NewString() + "/versions"
	at := time.Date(2026, 11, 17, 9, 30, 1, 123456000, time.UTC)

	tests := []struct {
		name     string
		fields   map[string]string
		wantNil  bool
		wantTime *time.Time
	}{
		{name: "not sent", fields: map[string]string{}, wantNil: true},
		{name: "no draft expected", fields: map[string]string{"expected_draft": "none"}},
		{name: "draft expected", fields: map[string]string{"expected_draft": "2026-11-17T14:30:01.123456+05:00"}, wantTime: &at},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newAdminTestServer(t)
			token := s.login()
			tt.fields["schema"] = `{"type":"object"}`
			body, ct := multipartBody(t, []byte("<p>x</p>"), tt.fields)
			if rec := s.upload(path, body, ct, token); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}

			got := s.templates.gotUpload.Expect
			switch {
			case tt.wantNil:
				if got != nil {
					t.Errorf("Expect = %+v, want nil", got)
				}
			case got == nil:
				t.Fatal("Expect = nil")
			case tt.wantTime == nil && got.UpdatedAt != nil:
				t.Errorf("UpdatedAt = %v, want nil", got.UpdatedAt)
			case tt.wantTime != nil && (got.UpdatedAt == nil || !got.UpdatedAt.Equal(*tt.wantTime)):
				t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, tt.wantTime)
			}
		})
	}

	t.Run("invalid", func(t *testing.T) {
		s := newAdminTestServer(t)
		token := s.login()
		body, ct := multipartBody(t, []byte("<p>x</p>"), map[string]string{"schema": "{}", "expected_draft": "yesterday"})
		assertErrorCode(t, s.upload(path, body, ct, token), http.StatusBadRequest, "invalid_expected_draft")
	})
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()

	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, wantStatus, rec.Body.String())
	}

	var body errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != wantCode {
		t.Errorf("error code = %q, want %q", body.Error.Code, wantCode)
	}
}
