package db_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"docgen/internal/db"
)

// TestJournalQueries проверяет фильтры журнала, страницы и отчёты на настоящей базе
// (в откатываемой транзакции).
func TestJournalQueries(t *testing.T) {
	if os.Getenv("DOCGEN_INTEGRATION") != "1" {
		t.Skip("set DOCGEN_INTEGRATION=1 to run integration tests")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, os.Getenv("DB_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	q := db.New(tx)

	project := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := q.CreateProject(ctx, db.CreateProjectParams{Code: project, Name: "Тест"}); err != nil {
		t.Fatal(err)
	}
	client, err := q.CreateAPIClient(ctx, db.CreateAPIClientParams{Name: "p", Project: project, KeyHash: []byte(project), KeyPrefix: "dg_t"})
	if err != nil {
		t.Fatal(err)
	}

	// 2026-11-16 23:30 по Астане — это 18:30 UTC того же дня; 2026-11-17 00:30 по Астане — 19:30 UTC 16-го.
	almaty := time.FixedZone("Asia/Almaty", 5*3600)
	entries := []struct {
		at       time.Time
		status   string
		template string
		ms       int32
	}{
		{time.Date(2026, 11, 16, 23, 30, 0, 0, almaty), "created", "opening_protocol", 100},
		{time.Date(2026, 11, 17, 0, 30, 0, 0, almaty), "created", "opening_protocol", 300},
		{time.Date(2026, 11, 17, 10, 0, 0, 0, almaty), "rejected", "opening_protocol", 5},
		{time.Date(2026, 11, 17, 11, 0, 0, 0, almaty), "repeated", "result_protocol", 2},
		{time.Date(2026, 11, 17, 12, 0, 0, 0, almaty), "failed", "result_protocol", 50},
	}
	for i, e := range entries {
		name := e.template
		var code *string
		if e.status == "rejected" || e.status == "failed" {
			c := "some_error"
			code = &c
		}
		if _, err := tx.Exec(ctx, `INSERT INTO generation_log
			(created_at, api_client_id, project, request_id, template_name, status, http_status, error_code, duration_ms)
			VALUES ($1, $2, $3, $4, $5, $6, 200, $7, $8)`,
			e.at, client.ID, project, fmt.Sprintf("r%d", i), name, e.status, code, e.ms); err != nil {
			t.Fatal(err)
		}
	}

	list := func(p db.ListGenerationLogParams) []db.ListGenerationLogRow {
		t.Helper()
		p.Project = &project
		if p.Statuses == nil {
			p.Statuses = []string{}
		}
		if p.PageSize == 0 {
			p.PageSize = 50
		}
		rows, err := q.ListGenerationLog(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}

	if rows := list(db.ListGenerationLogParams{}); len(rows) != 5 || rows[0].GenerationLog.RequestID != "r4" || rows[0].ProjectName != "Тест" {
		t.Errorf("all: %d rows, first %s", len(rows), rows[0].GenerationLog.RequestID)
	}
	if rows := list(db.ListGenerationLogParams{Statuses: []string{"rejected", "failed"}}); len(rows) != 2 {
		t.Errorf("errors: %d rows", len(rows))
	}
	tmpl := "result_protocol"
	if rows := list(db.ListGenerationLogParams{TemplateName: &tmpl}); len(rows) != 2 {
		t.Errorf("by template: %d rows", len(rows))
	}
	from, to := time.Date(2026, 11, 17, 0, 0, 0, 0, almaty), time.Date(2026, 11, 18, 0, 0, 0, 0, almaty)
	if rows := list(db.ListGenerationLogParams{FromTime: &from, ToTime: &to}); len(rows) != 4 {
		t.Errorf("17 Nov in Astana: %d rows", len(rows))
	}

	// Страницы по 2: r4 r3 | r2 r1 | r0.
	var seen []string
	page := db.ListGenerationLogParams{PageSize: 2}
	for {
		rows := list(page)
		for _, r := range rows {
			seen = append(seen, r.GenerationLog.RequestID)
		}
		if len(rows) < 2 {
			break
		}
		last := rows[len(rows)-1].GenerationLog
		page.AfterTime, page.AfterID = &last.CreatedAt, &last.ID
	}
	if fmt.Sprint(seen) != "[r4 r3 r2 r1 r0]" {
		t.Errorf("pages: %v", seen)
	}

	period := struct{ from, to time.Time }{time.Date(2026, 11, 1, 0, 0, 0, 0, almaty), to}
	totals, err := q.ReportTotals(ctx, db.ReportTotalsParams{FromTime: period.from, ToTime: period.to, Project: &project})
	if err != nil {
		t.Fatal(err)
	}
	if totals.Requests != 5 || totals.Created != 2 || totals.Repeated != 1 || totals.Rejected != 1 || totals.Failed != 1 || totals.AvgDurationMs != 200 {
		t.Errorf("totals = %+v", totals)
	}

	byTemplate, err := q.ReportByTemplate(ctx, db.ReportByTemplateParams{FromTime: period.from, ToTime: period.to, Project: &project})
	if err != nil || len(byTemplate) != 2 || byTemplate[0].TemplateName != "opening_protocol" || byTemplate[0].Created != 2 || byTemplate[0].Errors != 1 {
		t.Errorf("by template = %+v, %v", byTemplate, err)
	}

	byDay, err := q.ReportByDay(ctx, db.ReportByDayParams{FromTime: period.from, ToTime: period.to, Project: &project})
	if err != nil {
		t.Fatal(err)
	}
	days := map[string][2]int64{}
	for _, d := range byDay {
		days[d.Day.Time.Format(time.DateOnly)] = [2]int64{d.Created, d.Errors}
	}
	// 23:30 16-го и 00:30 17-го по Астане — разные дни, хотя в UTC оба 16-го.
	if days["2026-11-16"] != [2]int64{1, 0} || days["2026-11-17"] != [2]int64{1, 2} {
		t.Errorf("by day = %v", days)
	}

	if _, err := q.GetGenerationLog(ctx, list(db.ListGenerationLogParams{})[0].GenerationLog.ID); err != nil {
		t.Errorf("GetGenerationLog: %v", err)
	}
}
