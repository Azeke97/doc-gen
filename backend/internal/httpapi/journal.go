package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"docgen/internal/db"
	"docgen/internal/format"
)

const (
	defaultJournalPage = 50
	maxJournalPage     = 200
	// defaultReportDays — период отчёта, если он не задан: последние 30 дней, включая сегодня.
	defaultReportDays = 30
)

var journalStatuses = map[string]bool{"created": true, "repeated": true, "rejected": true, "failed": true}

// JournalReader — то, что журналу и отчётам админки нужно от базы.
type JournalReader interface {
	ListGenerationLog(ctx context.Context, arg db.ListGenerationLogParams) ([]db.ListGenerationLogRow, error)
	GetGenerationLog(ctx context.Context, id uuid.UUID) (db.GetGenerationLogRow, error)
	GetDocument(ctx context.Context, id uuid.UUID) (db.Document, error)
	ReportTotals(ctx context.Context, arg db.ReportTotalsParams) (db.ReportTotalsRow, error)
	ReportByProject(ctx context.Context, arg db.ReportByProjectParams) ([]db.ReportByProjectRow, error)
	ReportByTemplate(ctx context.Context, arg db.ReportByTemplateParams) ([]db.ReportByTemplateRow, error)
	ReportByDay(ctx context.Context, arg db.ReportByDayParams) ([]db.ReportByDayRow, error)
}

type journalEntry struct {
	ID             uuid.UUID       `json:"id"`
	CreatedAt      time.Time       `json:"created_at"`
	Project        string          `json:"project"`
	ProjectName    string          `json:"project_name"`
	RequestID      string          `json:"request_id"`
	TemplateName   *string         `json:"template_name"`
	TemplateSHA256 *string         `json:"template_sha256"`
	Format         *string         `json:"format"`
	ExternalID     *string         `json:"external_id"`
	RequestedBy    *string         `json:"requested_by"`
	IdempotencyKey *string         `json:"idempotency_key"`
	Status         string          `json:"status"`
	HTTPStatus     int32           `json:"http_status"`
	ErrorCode      *string         `json:"error_code"`
	ErrorMessage   *string         `json:"error_message"`
	ErrorDetails   json.RawMessage `json:"error_details"`
	DocumentID     *uuid.UUID      `json:"document_id"`
	ResultSize     *int64          `json:"result_size"`
	DurationMs     int32           `json:"duration_ms"`
}

func toJournalEntry(g db.GenerationLog, projectName string) journalEntry {
	e := journalEntry{
		ID: g.ID, CreatedAt: g.CreatedAt, Project: g.Project, ProjectName: projectName, RequestID: g.RequestID,
		TemplateName: g.TemplateName, Format: g.Format, ExternalID: g.ExternalID, RequestedBy: g.RequestedBy,
		IdempotencyKey: g.IdempotencyKey, Status: g.Status, HTTPStatus: g.HTTPStatus, ErrorCode: g.ErrorCode,
		ErrorMessage: g.ErrorMessage, ErrorDetails: g.ErrorDetails, DocumentID: g.DocumentID, ResultSize: g.ResultSize,
		DurationMs: g.DurationMs,
	}
	if g.TemplateSha256 != nil {
		sum := hex.EncodeToString(g.TemplateSha256)
		e.TemplateSHA256 = &sum
	}
	return e
}

// journalHandler — GET /admin/v1/journal: журнал запросов с фильтрами, новые первыми.
// Следующая страница — с ?cursor= из next_cursor предыдущей.
func journalHandler(store JournalReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		params := db.ListGenerationLogParams{
			Project:      queryOptional(q, "project"),
			TemplateName: queryOptional(q, "template_name"),
			ExternalID:   queryOptional(q, "external_id"),
			Statuses:     []string{},
			PageSize:     defaultJournalPage,
		}
		for _, s := range q["status"] {
			if !journalStatuses[s] {
				writeError(w, r, http.StatusBadRequest, "invalid_status", "status must be created, repeated, rejected or failed")
				return
			}
			params.Statuses = append(params.Statuses, s)
		}

		from, to, err := parsePeriod(q)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_period", err.Error())
			return
		}
		params.FromTime, params.ToTime = from, to

		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > maxJournalPage {
				writeError(w, r, http.StatusBadRequest, "invalid_limit", fmt.Sprintf("limit must be from 1 to %d", maxJournalPage))
				return
			}
			params.PageSize = int32(n) //nolint:gosec // проверено выше
		}
		if v := q.Get("cursor"); v != "" {
			at, id, err := decodeCursor(v)
			if err != nil {
				writeError(w, r, http.StatusBadRequest, "invalid_cursor", "cursor is not valid")
				return
			}
			params.AfterTime, params.AfterID = &at, &id
		}

		rows, err := store.ListGenerationLog(r.Context(), params)
		if err != nil {
			slog.ErrorContext(r.Context(), "list journal", "err", err)
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		resp := struct {
			Items      []journalEntry `json:"items"`
			NextCursor *string        `json:"next_cursor"`
		}{Items: make([]journalEntry, 0, len(rows))}
		for _, row := range rows {
			resp.Items = append(resp.Items, toJournalEntry(row.GenerationLog, row.ProjectName))
		}
		if len(rows) == int(params.PageSize) {
			last := rows[len(rows)-1].GenerationLog
			c := encodeCursor(last.CreatedAt, last.ID)
			resp.NextCursor = &c
		}
		writeJSON(w, r, http.StatusOK, resp)
	}
}

// journalDocument — выпущенный документ в карточке запроса. Data — входные данные,
// пока документ хранится; после срока хранения их нет.
type journalDocument struct {
	ID         uuid.UUID       `json:"id"`
	Format     string          `json:"format"`
	FileSize   int64           `json:"file_size"`
	FileSHA256 string          `json:"file_sha256"`
	CreatedAt  time.Time       `json:"created_at"`
	ExpiresAt  time.Time       `json:"expires_at"`
	Expired    bool            `json:"expired"`
	Data       json.RawMessage `json:"data"`
}

// journalEntryHandler — GET /admin/v1/journal/{id}: карточка запроса.
// document_state: available — документ хранится, expired — срок истёк, deleted — удалён по сроку,
// null — документа не было (запрос отклонён).
func journalEntryHandler(store JournalReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}
		row, err := store.GetGenerationLog(r.Context(), id)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "journal_entry_not_found", "journal entry not found")
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "get journal entry", "err", err)
			writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			return
		}

		resp := struct {
			Entry         journalEntry     `json:"entry"`
			DocumentState *string          `json:"document_state"`
			Document      *journalDocument `json:"document"`
		}{Entry: toJournalEntry(row.GenerationLog, row.ProjectName)}

		g := row.GenerationLog
		state := func(s string) *string { return &s }
		switch {
		case g.DocumentID != nil:
			doc, err := store.GetDocument(r.Context(), *g.DocumentID)
			if errors.Is(err, pgx.ErrNoRows) {
				resp.DocumentState = state("deleted")
				break
			}
			if err != nil {
				slog.ErrorContext(r.Context(), "get journal document", "err", err)
				writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
				return
			}
			d := &journalDocument{
				ID: doc.ID, Format: doc.Format, FileSize: doc.FileSize, FileSHA256: hex.EncodeToString(doc.FileSha256),
				CreatedAt: doc.CreatedAt, ExpiresAt: doc.ExpiresAt, Expired: !time.Now().Before(doc.ExpiresAt),
			}
			resp.DocumentState = state("available")
			if d.Expired {
				resp.DocumentState = state("expired")
			} else {
				d.Data = doc.Data
			}
			resp.Document = d
		case g.Status == "created" || g.Status == "repeated":
			resp.DocumentState = state("deleted")
		}

		writeJSON(w, r, http.StatusOK, resp)
	}
}

type reportResponse struct {
	From       time.Time      `json:"from"`
	To         time.Time      `json:"to"` // не включая
	Totals     reportTotals   `json:"totals"`
	ByProject  []reportGroup  `json:"by_project"`
	ByTemplate []reportGroup  `json:"by_template"`
	ByDay      []reportDayRow `json:"by_day"`
}

type reportTotals struct {
	Requests      int64 `json:"requests"`
	Created       int64 `json:"created"`
	Repeated      int64 `json:"repeated"`
	Rejected      int64 `json:"rejected"`
	Failed        int64 `json:"failed"`
	AvgDurationMs int32 `json:"avg_duration_ms"` // среднее время выпуска нового документа
}

type reportGroup struct {
	Project       string  `json:"project"`
	ProjectName   *string `json:"project_name,omitempty"`
	TemplateName  *string `json:"template_name,omitempty"`
	Created       int64   `json:"created"`
	Errors        int64   `json:"errors"`
	AvgDurationMs *int32  `json:"avg_duration_ms,omitempty"`
}

type reportDayRow struct {
	Day     string `json:"day"`
	Created int64  `json:"created"`
	Errors  int64  `json:"errors"`
}

// reportsHandler — GET /admin/v1/reports?from=&to=&project=: число документов и ошибок
// по порталам, шаблонам и дням, среднее время генерации. Без периода — последние 30 дней.
func reportsHandler(store JournalReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		from, to, err := parsePeriod(q)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_period", err.Error())
			return
		}
		if to == nil {
			end := startOfDay(time.Now()).AddDate(0, 0, 1)
			to = &end
		}
		if from == nil {
			start := to.AddDate(0, 0, -defaultReportDays)
			from = &start
		}
		project := queryOptional(q, "project")

		ctx := r.Context()
		totals, err := store.ReportTotals(ctx, db.ReportTotalsParams{FromTime: *from, ToTime: *to, Project: project})
		if err != nil {
			reportFailed(w, r, err)
			return
		}
		byProject, err := store.ReportByProject(ctx, db.ReportByProjectParams{FromTime: *from, ToTime: *to, Project: project})
		if err != nil {
			reportFailed(w, r, err)
			return
		}
		byTemplate, err := store.ReportByTemplate(ctx, db.ReportByTemplateParams{FromTime: *from, ToTime: *to, Project: project})
		if err != nil {
			reportFailed(w, r, err)
			return
		}
		byDay, err := store.ReportByDay(ctx, db.ReportByDayParams{FromTime: *from, ToTime: *to, Project: project})
		if err != nil {
			reportFailed(w, r, err)
			return
		}

		resp := reportResponse{
			From: *from, To: *to,
			Totals:     reportTotals(totals),
			ByProject:  make([]reportGroup, 0, len(byProject)),
			ByTemplate: make([]reportGroup, 0, len(byTemplate)),
			ByDay:      make([]reportDayRow, 0, len(byDay)),
		}
		for _, p := range byProject {
			resp.ByProject = append(resp.ByProject, reportGroup{Project: p.Project, ProjectName: &p.ProjectName, Created: p.Created, Errors: p.Errors})
		}
		for _, t := range byTemplate {
			resp.ByTemplate = append(resp.ByTemplate, reportGroup{
				Project: t.Project, TemplateName: &t.TemplateName, Created: t.Created, Errors: t.Errors, AvgDurationMs: &t.AvgDurationMs,
			})
		}
		for _, d := range byDay {
			resp.ByDay = append(resp.ByDay, reportDayRow{Day: d.Day.Time.Format(time.DateOnly), Created: d.Created, Errors: d.Errors})
		}
		writeJSON(w, r, http.StatusOK, resp)
	}
}

func reportFailed(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "build report", "err", err)
	writeError(w, r, http.StatusInternalServerError, "internal", "internal server error")
}

// parsePeriod читает from и to: дата 2026-11-01 (день по Астане; to включает весь день)
// или момент в RFC 3339. Возвращает полуинтервал [from, to).
func parsePeriod(q url.Values) (from, to *time.Time, err error) {
	parse := func(name string, end bool) (*time.Time, error) {
		v := q.Get(name)
		if v == "" {
			return nil, nil
		}
		if d, err := time.ParseInLocation(time.DateOnly, v, format.Kazakhstan); err == nil {
			if end {
				d = d.AddDate(0, 0, 1)
			}
			return &d, nil
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, fmt.Errorf("%s must be a date (2026-11-01) or RFC 3339 time", name)
		}
		return &t, nil
	}
	if from, err = parse("from", false); err != nil {
		return nil, nil, err
	}
	if to, err = parse("to", true); err != nil {
		return nil, nil, err
	}
	if from != nil && to != nil && !from.Before(*to) {
		return nil, nil, errors.New("from must be before to")
	}
	return from, to, nil
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.In(format.Kazakhstan).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, format.Kazakhstan)
}

func queryOptional(q url.Values, name string) *string {
	v := strings.TrimSpace(q.Get(name))
	if v == "" {
		return nil
	}
	return &v
}

// Курсор страницы: время и id последней записи, в base64, чтобы клиент передавал его как есть.
func encodeCursor(at time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

func decodeCursor(c string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, uuid.UUID{}, err
	}
	ts, idStr, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, uuid.UUID{}, errors.New("malformed cursor")
	}
	at, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, uuid.UUID{}, err
	}
	id, err := uuid.Parse(idStr)
	return at, id, err
}
