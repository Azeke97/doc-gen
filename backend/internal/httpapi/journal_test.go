package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"docgen/internal/db"
	"docgen/internal/format"
)

// fakeJournalReader отдаёт заданные записи и запоминает параметры запросов.
type fakeJournalReader struct {
	rows []db.ListGenerationLogRow
	doc  *db.Document

	gotList   db.ListGenerationLogParams
	gotReport db.ReportTotalsParams
}

func (f *fakeJournalReader) ListGenerationLog(_ context.Context, arg db.ListGenerationLogParams) ([]db.ListGenerationLogRow, error) {
	f.gotList = arg
	return f.rows, nil
}

func (f *fakeJournalReader) GetGenerationLog(_ context.Context, id uuid.UUID) (db.GetGenerationLogRow, error) {
	for _, r := range f.rows {
		if r.GenerationLog.ID == id {
			return db.GetGenerationLogRow(r), nil
		}
	}
	return db.GetGenerationLogRow{}, pgx.ErrNoRows
}

func (f *fakeJournalReader) GetDocument(_ context.Context, _ uuid.UUID) (db.Document, error) {
	if f.doc == nil {
		return db.Document{}, pgx.ErrNoRows
	}
	return *f.doc, nil
}

func (f *fakeJournalReader) ReportTotals(_ context.Context, arg db.ReportTotalsParams) (db.ReportTotalsRow, error) {
	f.gotReport = arg
	return db.ReportTotalsRow{Requests: 10, Created: 7, Repeated: 1, Rejected: 2, AvgDurationMs: 140}, nil
}

func (f *fakeJournalReader) ReportByProject(context.Context, db.ReportByProjectParams) ([]db.ReportByProjectRow, error) {
	return []db.ReportByProjectRow{{Project: "procurement", ProjectName: "Закупки", Created: 7, Errors: 2}}, nil
}

func (f *fakeJournalReader) ReportByTemplate(context.Context, db.ReportByTemplateParams) ([]db.ReportByTemplateRow, error) {
	return []db.ReportByTemplateRow{{Project: "procurement", TemplateName: "opening_protocol", Created: 7, Errors: 2, AvgDurationMs: 140}}, nil
}

func (f *fakeJournalReader) ReportByDay(context.Context, db.ReportByDayParams) ([]db.ReportByDayRow, error) {
	day := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	return []db.ReportByDayRow{{Day: pgtype.Date{Time: day, Valid: true}, Created: 7, Errors: 2}}, nil
}

func logRow(status string, docID *uuid.UUID) db.ListGenerationLogRow {
	name := "opening_protocol"
	g := db.GenerationLog{
		ID: uuid.New(), CreatedAt: time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC), Project: "procurement",
		RequestID: "req-1", TemplateName: &name, TemplateSha256: make([]byte, 32), Status: status, HTTPStatus: 201,
		DocumentID: docID, DurationMs: 120,
	}
	if status == "rejected" {
		code := "invalid_data"
		g.HTTPStatus, g.ErrorCode, g.ErrorDetails = 400, &code, json.RawMessage(`[{"field":"lots[1].name","message":"нет в данных"}]`)
	}
	return db.ListGenerationLogRow{GenerationLog: g, ProjectName: "Закупки"}
}

func TestJournalFilters(t *testing.T) {
	s := newAdminTestServer(t)
	s.journal.rows = []db.ListGenerationLogRow{logRow("created", nil), logRow("rejected", nil)}
	token := s.login()

	rec := s.do(http.MethodGet, "/admin/v1/journal?project=procurement&template_name=opening_protocol&external_id=lot:1"+
		"&status=rejected&status=failed&from=2026-11-01&to=2026-11-17&limit=2", "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}

	got := s.journal.gotList
	almaty := format.Kazakhstan
	if *got.Project != "procurement" || *got.TemplateName != "opening_protocol" || *got.ExternalID != "lot:1" ||
		strings.Join(got.Statuses, ",") != "rejected,failed" || got.PageSize != 2 ||
		!got.FromTime.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, almaty)) ||
		!got.ToTime.Equal(time.Date(2026, 11, 18, 0, 0, 0, 0, almaty)) {
		t.Errorf("params = %+v", got)
	}

	var body struct {
		Items      []journalEntry `json:"items"`
		NextCursor *string        `json:"next_cursor"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 2 || body.Items[1].ErrorCode == nil || body.Items[0].ProjectName != "Закупки" || body.NextCursor == nil {
		t.Fatalf("body = %+v", body)
	}

	// Курсор ведёт на страницу после последней записи.
	s.do(http.MethodGet, "/admin/v1/journal?cursor="+*body.NextCursor, "", token)
	last := s.journal.rows[1].GenerationLog
	if s.journal.gotList.AfterID == nil || *s.journal.gotList.AfterID != last.ID || !s.journal.gotList.AfterTime.Equal(last.CreatedAt) {
		t.Errorf("cursor params = %+v", s.journal.gotList)
	}
}

func TestJournalBadParams(t *testing.T) {
	s := newAdminTestServer(t)
	token := s.login()

	for path, code := range map[string]string{
		"/admin/v1/journal?status=ok":                            "invalid_status",
		"/admin/v1/journal?from=вчера":                           "invalid_period",
		"/admin/v1/journal?from=2026-11-10&to=2026-11-01":        "invalid_period",
		"/admin/v1/journal?limit=1000":                           "invalid_limit",
		"/admin/v1/journal?cursor=***":                           "invalid_cursor",
		"/admin/v1/reports?from=2026-13-01":                      "invalid_period",
		"/admin/v1/journal/" + uuid.NewString():                  "journal_entry_not_found",
		"/admin/v1/journal?from=2026-11-01T00:00:00%2B05:00&to=": "",
	} {
		rec := s.do(http.MethodGet, path, "", token)
		if code == "" {
			if rec.Code != http.StatusOK {
				t.Errorf("%s: status = %d", path, rec.Code)
			}
			continue
		}
		var body errorResponse
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body.Error.Code != code {
			t.Errorf("%s: code = %q, want %q", path, body.Error.Code, code)
		}
	}

	if rec := s.do(http.MethodGet, "/admin/v1/journal", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("journal without session: status = %d", rec.Code)
	}
}

func TestJournalEntryCard(t *testing.T) {
	docID := uuid.New()
	s := newAdminTestServer(t)
	created, rejected, gone := logRow("created", &docID), logRow("rejected", nil), logRow("created", nil)
	s.journal.rows = []db.ListGenerationLogRow{created, rejected, gone}
	token := s.login()

	card := func(id uuid.UUID) map[string]any {
		t.Helper()
		rec := s.do(http.MethodGet, "/admin/v1/journal/"+id.String(), "", token)
		var body map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("card: status %d, %v", rec.Code, err)
		}
		return body
	}

	// Документ хранится — в карточке входные данные.
	s.journal.doc = &db.Document{ID: docID, Format: "pdf", FileSha256: make([]byte, 32), Data: json.RawMessage(`{"number":"0001"}`),
		ExpiresAt: time.Now().Add(time.Hour)}
	body := card(created.GenerationLog.ID)
	doc, _ := body["document"].(map[string]any)
	if body["document_state"] != "available" || doc == nil || doc["data"].(map[string]any)["number"] != "0001" {
		t.Errorf("available: %v", body)
	}

	// Срок истёк — данных уже не показываем.
	s.journal.doc.ExpiresAt = time.Now().Add(-time.Hour)
	body = card(created.GenerationLog.ID)
	if doc, _ := body["document"].(map[string]any); body["document_state"] != "expired" || doc["data"] != nil {
		t.Errorf("expired: %v", body)
	}

	// Удалён по сроку хранения: ссылка в журнале обнулилась.
	body = card(gone.GenerationLog.ID)
	if body["document_state"] != "deleted" || body["document"] != nil {
		t.Errorf("deleted: %v", body)
	}

	// Отклонённый запрос: документа не было, есть ошибки.
	body = card(rejected.GenerationLog.ID)
	entry := body["entry"].(map[string]any)
	if body["document_state"] != nil || entry["error_code"] != "invalid_data" || entry["error_details"] == nil {
		t.Errorf("rejected: %v", body)
	}
}

func TestReports(t *testing.T) {
	s := newAdminTestServer(t)
	token := s.login()

	rec := s.do(http.MethodGet, "/admin/v1/reports?project=procurement", "", token)
	var body reportResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d, %v", rec.Code, err)
	}
	if body.Totals.Created != 7 || body.Totals.Rejected != 2 || len(body.ByProject) != 1 || *body.ByTemplate[0].TemplateName != "opening_protocol" ||
		body.ByDay[0].Day != "2026-11-17" {
		t.Errorf("body = %+v", body)
	}

	// Без периода — последние 30 дней по Астане, включая сегодня.
	got := s.journal.gotReport
	if days := got.ToTime.Sub(got.FromTime).Hours() / 24; days != 30 || *got.Project != "procurement" {
		t.Errorf("period = %v .. %v (%.0f days)", got.FromTime, got.ToTime, days)
	}
	if !got.ToTime.After(time.Now()) || time.Until(got.ToTime) > 24*time.Hour {
		t.Errorf("to = %v, want the end of today", got.ToTime)
	}
}

func TestAdminAuthDisabled(t *testing.T) {
	get := func(disabled bool, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		NewRouter(Deps{
			AdminAuth: newFakeAdminAuthStore(t, "admin@example.com", "secret-password"), JournalReader: &fakeJournalReader{},
			AdminAuthDisabled: disabled,
		}).ServeHTTP(rec, req)
		return rec
	}

	if got := strings.TrimSpace(get(false, "/admin/v1/login-info").Body.String()); got != `{"auth_required":true}` {
		t.Errorf("login-info with auth: %s", got)
	}
	if rec := get(false, "/admin/v1/journal"); rec.Code != http.StatusUnauthorized {
		t.Errorf("journal with auth, no session: status = %d", rec.Code)
	}

	if got := strings.TrimSpace(get(true, "/admin/v1/login-info").Body.String()); got != `{"auth_required":false}` {
		t.Errorf("login-info without auth: %s", got)
	}
	if rec := get(true, "/admin/v1/journal"); rec.Code != http.StatusOK {
		t.Errorf("journal without auth: status = %d", rec.Code)
	}
	rec := get(true, "/admin/v1/me")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Демо-режим") {
		t.Errorf("me without auth: %d %s", rec.Code, rec.Body)
	}
}
