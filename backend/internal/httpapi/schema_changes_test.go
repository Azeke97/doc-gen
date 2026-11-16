package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"docgen/internal/dataschema"
	"docgen/internal/templates"
)

var breakingDiff = templates.VersionDiff{
	BaseVersion: new(int32(1)),
	Changes: []dataschema.Change{
		{Path: "lots[].amount", Kind: dataschema.KindTypeChanged, Breaking: true, Message: "type changed from number to string"},
	},
}

func TestPublishBreakingChanges(t *testing.T) {
	s := newAdminTestServer(t)
	s.templates.err = &templates.BreakingChangesError{Diff: breakingDiff}
	token := s.login()

	rec := s.do(http.MethodPost, "/admin/v1/versions/"+uuid.NewString()+"/publish", "", token)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body: %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code    string       `json:"code"`
			Details diffResponse `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "breaking_changes" || !body.Error.Details.Breaking || len(body.Error.Details.Changes) != 1 {
		t.Errorf("body = %+v, want breaking_changes with one change", body.Error)
	}
	if s.templates.gotOpts.ConfirmBreaking {
		t.Error("ConfirmBreaking = true without query parameter")
	}
}

func TestPublishConfirmBreaking(t *testing.T) {
	s := newAdminTestServer(t)
	token := s.login()

	rec := s.do(http.MethodPost, "/admin/v1/versions/"+uuid.NewString()+"/publish?confirm_breaking=true", "", token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !s.templates.gotOpts.ConfirmBreaking {
		t.Error("ConfirmBreaking = false, want true")
	}
}

func TestDiffVersion(t *testing.T) {
	s := newAdminTestServer(t)
	s.templates.diff = breakingDiff
	token := s.login()
	versionID := uuid.New()

	rec := s.do(http.MethodGet, "/admin/v1/versions/"+versionID.String()+"/diff", "", token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var body diffResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.BaseVersion == nil || *body.BaseVersion != 1 || !body.Breaking || len(body.Changes) != 1 {
		t.Errorf("body = %+v", body)
	}
	if s.templates.gotIDs[0] != versionID {
		t.Errorf("version id = %s, want %s", s.templates.gotIDs[0], versionID)
	}
}

func TestDiffVersionNoBase(t *testing.T) {
	s := newAdminTestServer(t)
	s.templates.diff = templates.VersionDiff{Changes: []dataschema.Change{}}
	token := s.login()

	rec := s.do(http.MethodGet, "/admin/v1/versions/"+uuid.NewString()+"/diff", "", token)

	// Первая публикация: сравнивать не с чем, но поля в ответе есть всегда.
	if got := rec.Body.String(); got != `{"base_version":null,"breaking":false,"changes":[]}`+"\n" {
		t.Errorf("body = %s", got)
	}
}

func TestUploadTestDataErrorsInDetails(t *testing.T) {
	s := newAdminTestServer(t)
	s.templates.err = &templates.ValidationError{
		Field:   "test_data",
		Message: "does not match schema",
		Errors:  []dataschema.FieldError{{Field: "/number", Message: "is required"}},
	}
	token := s.login()

	body, ct := multipartBody(t, []byte("<p></p>"), map[string]string{"schema": `{"type":"object"}`, "test_data": `{}`})
	rec := s.upload("/admin/v1/templates/"+uuid.NewString()+"/versions", body, ct, token)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var resp struct {
		Error struct {
			Code    string                  `json:"code"`
			Details []dataschema.FieldError `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != "invalid_test_data" || len(resp.Error.Details) != 1 || resp.Error.Details[0].Field != "/number" {
		t.Errorf("error = %+v", resp.Error)
	}
}
