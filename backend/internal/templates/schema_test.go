package templates

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"docgen/internal/dataschema"
	"docgen/internal/db"
	"docgen/internal/storage"
)

// publishActive загружает версию со схемой, публикует и делает активной.
func publishActive(t *testing.T, svc *Service, tmplID uuid.UUID, schema string) db.TemplateVersion {
	t.Helper()
	ctx := t.Context()

	v, _, err := svc.UploadVersion(ctx, "alice", tmplID, UploadParams{File: testHTML, Schema: json.RawMessage(schema)})
	if err != nil {
		t.Fatalf("UploadVersion: %v", err)
	}
	if _, err := svc.Publish(ctx, "bob", v.ID, PublishOptions{ConfirmBreaking: true}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := svc.Activate(ctx, "bob", tmplID, v.ID, ActivateOptions{}); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	return v
}

func TestUploadChecksSchema(t *testing.T) {
	svc, _, project := newTestService(t)
	ctx := t.Context()

	tmpl, err := svc.Create(ctx, "alice", validParams(project))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tests := []struct {
		name      string
		schema    string
		testData  string
		wantField string
		wantErrs  int
	}{
		{name: "root not object", schema: `{"type":"array"}`, wantField: "schema"},
		{name: "external ref", schema: `{"type":"object","properties":{"a":{"$ref":"file:///etc/passwd"}}}`, wantField: "schema"},
		{name: "test data not json", schema: string(testSchema), testData: `{`, wantField: "test_data"},
		{name: "test data mismatch", schema: `{"type":"object","required":["title"]}`, testData: `{}`, wantField: "test_data", wantErrs: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := UploadParams{File: testHTML, Schema: json.RawMessage(tt.schema)}
			if tt.testData != "" {
				p.TestData = json.RawMessage(tt.testData)
			}

			_, _, err := svc.UploadVersion(ctx, "alice", tmpl.ID, p)

			var vErr *ValidationError
			if !errors.As(err, &vErr) || vErr.Field != tt.wantField {
				t.Fatalf("err = %v, want ValidationError on %s", err, tt.wantField)
			}
			if len(vErr.Errors) != tt.wantErrs {
				t.Errorf("field errors = %+v, want %d", vErr.Errors, tt.wantErrs)
			}
		})
	}

	// Подходящие тестовые данные принимаются.
	_, _, err = svc.UploadVersion(ctx, "alice", tmpl.ID, UploadParams{
		File: testHTML, Schema: testSchema, TestData: json.RawMessage(`{"title":"Протокол"}`),
	})
	if err != nil {
		t.Errorf("valid test data: %v", err)
	}
}

func TestResolve(t *testing.T) {
	svc, _, project := newTestService(t)
	ctx := t.Context()

	p := validParams(project)
	p.Locale = "ru"
	ru, err := svc.Create(ctx, "alice", p)
	if err != nil {
		t.Fatalf("Create ru: %v", err)
	}
	v1 := publishActive(t, svc, ru.ID, string(testSchema))
	v2 := publishActive(t, svc, ru.ID, `{"type":"object","properties":{"title":{"type":"string"},"date":{"type":"string"}}}`)

	// Черновик v3 порталу не виден даже по номеру.
	if _, _, err := svc.UploadVersion(ctx, "alice", ru.ID, UploadParams{File: testHTML, Schema: testSchema}); err != nil {
		t.Fatalf("Upload v3 draft: %v", err)
	}

	got, err := svc.Resolve(ctx, project, Ref{Code: p.Code})
	if err != nil {
		t.Fatalf("Resolve active: %v", err)
	}
	if got.VersionID != v2.ID || got.Locale != "ru" {
		t.Errorf("active = v%d %s, want v2 ru", got.Version, got.Locale)
	}

	got, err = svc.Resolve(ctx, project, Ref{Code: p.Code, Version: 1})
	if err != nil {
		t.Fatalf("Resolve v1: %v", err)
	}
	if got.VersionID != v1.ID {
		t.Errorf("pinned = v%d, want v1", got.Version)
	}

	if _, err := svc.Resolve(ctx, project, Ref{Code: p.Code, Version: 3}); !errors.Is(err, ErrVersionNotFound) {
		t.Errorf("draft v3: err = %v, want ErrVersionNotFound", err)
	}
	if _, err := svc.Resolve(ctx, project+"_other", Ref{Code: p.Code}); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("other project: err = %v, want ErrTemplateNotFound", err)
	}

	// Вторая локаль того же кода: без locale выбрать нельзя, с locale — можно.
	p.Locale = "kk"
	kk, err := svc.Create(ctx, "alice", p)
	if err != nil {
		t.Fatalf("Create kk: %v", err)
	}
	publishActive(t, svc, kk.ID, string(testSchema))

	var vErr *ValidationError
	if _, err := svc.Resolve(ctx, project, Ref{Code: p.Code}); !errors.As(err, &vErr) || vErr.Field != "locale" {
		t.Errorf("two locales: err = %v, want ValidationError on locale", err)
	}
	got, err = svc.Resolve(ctx, project, Ref{Code: p.Code, Locale: "kk"})
	if err != nil || got.Locale != "kk" {
		t.Errorf("Resolve kk = %+v, %v; want kk", got, err)
	}
}

func TestPublishBreakingChanges(t *testing.T) {
	svc, tx, project := newTestService(t)
	ctx := t.Context()

	tmpl, err := svc.Create(ctx, "alice", validParams(project))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Первая версия: сравнивать не с чем.
	v1, _, err := svc.UploadVersion(ctx, "alice", tmpl.ID, UploadParams{File: testHTML, Schema: testSchema})
	if err != nil {
		t.Fatalf("Upload v1: %v", err)
	}
	diff, err := svc.DiffVersion(ctx, v1.ID)
	if err != nil || diff.BaseVersion != nil || len(diff.Changes) != 0 {
		t.Fatalf("DiffVersion v1 = %+v, %v; want no base", diff, err)
	}
	if _, err := svc.Publish(ctx, "bob", v1.ID, PublishOptions{}); err != nil {
		t.Fatalf("Publish v1: %v", err)
	}
	if _, err := svc.Activate(ctx, "bob", tmpl.ID, v1.ID, ActivateOptions{}); err != nil {
		t.Fatalf("Activate v1: %v", err)
	}

	// v2 требует новое поле: портал, который шлёт данные под v1, получит ошибки.
	v2, _, err := svc.UploadVersion(ctx, "alice", tmpl.ID, UploadParams{
		File:   testHTML,
		Schema: json.RawMessage(`{"type":"object","required":["number"],"properties":{"title":{"type":"string"},"number":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("Upload v2: %v", err)
	}

	wantChanges := []dataschema.Change{{Path: "number", Kind: dataschema.KindFieldAdded, Breaking: true, Message: "new required field"}}

	_, err = svc.Publish(ctx, "bob", v2.ID, PublishOptions{})
	var bErr *BreakingChangesError
	if !errors.As(err, &bErr) {
		t.Fatalf("Publish v2: err = %v, want BreakingChangesError", err)
	}
	if bErr.Diff.BaseVersion == nil || *bErr.Diff.BaseVersion != 1 || len(bErr.Diff.Changes) != 1 || bErr.Diff.Changes[0] != wantChanges[0] {
		t.Errorf("diff = %+v, want base 1 and %+v", bErr.Diff, wantChanges)
	}

	// Публикация не случилась: версия осталась черновиком.
	var status string
	if err := tx.QueryRow(ctx, "SELECT status FROM template_versions WHERE id = $1", v2.ID).Scan(&status); err != nil || status != "draft" {
		t.Fatalf("v2 status = %q, %v; want draft", status, err)
	}

	if _, err := svc.Publish(ctx, "bob", v2.ID, PublishOptions{ConfirmBreaking: true}); err != nil {
		t.Fatalf("Publish v2 confirmed: %v", err)
	}

	// В аудите видно, что ломающие изменения подтвердили осознанно.
	var details struct {
		BaseVersion       int32               `json:"base_version"`
		SchemaChanges     []dataschema.Change `json:"schema_changes"`
		BreakingConfirmed bool                `json:"breaking_confirmed"`
	}
	var raw []byte
	err = tx.QueryRow(ctx, "SELECT details FROM audit_log WHERE entity_id = $1 AND action = $2", v2.ID, ActionVersionPublished).Scan(&raw)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if !details.BreakingConfirmed || details.BaseVersion != 1 || len(details.SchemaChanges) != 1 {
		t.Errorf("audit details = %+v", details)
	}
}

func TestActivateRollbackBreaking(t *testing.T) {
	svc, _, project := newTestService(t)
	ctx := t.Context()

	tmpl, err := svc.Create(ctx, "alice", validParams(project))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// v1 требует title, v2 делает его необязательным: публикация v2 ничего не ломает,
	// а вот откат на v1 — ломает порталы, которые уже перестали слать title.
	v1 := publishActive(t, svc, tmpl.ID, `{"type":"object","required":["title"],"properties":{"title":{"type":"string"}}}`)
	publishActive(t, svc, tmpl.ID, `{"type":"object","properties":{"title":{"type":"string"}}}`)

	_, err = svc.Activate(ctx, "bob", tmpl.ID, v1.ID, ActivateOptions{})
	var bErr *BreakingChangesError
	if !errors.As(err, &bErr) {
		t.Fatalf("rollback: err = %v, want BreakingChangesError", err)
	}
	if bErr.Diff.BaseVersion == nil || *bErr.Diff.BaseVersion != 2 || len(bErr.Diff.Changes) != 1 || bErr.Diff.Changes[0].Kind != dataschema.KindBecameRequired {
		t.Errorf("diff = %+v, want base 2 and became_required", bErr.Diff)
	}

	rolledBack, err := svc.Activate(ctx, "bob", tmpl.ID, v1.ID, ActivateOptions{ConfirmBreaking: true})
	if err != nil {
		t.Fatalf("confirmed rollback: %v", err)
	}
	if *rolledBack.ActiveVersionID != v1.ID {
		t.Errorf("active = %s, want v1 %s", *rolledBack.ActiveVersionID, v1.ID)
	}
}

func TestReplaceDraftDeletesOldFile(t *testing.T) {
	svc, _, project := newTestService(t)
	ctx := t.Context()

	tmpl, first := uploadDraft(t, svc, project, uuid.Nil)

	second, created, err := svc.UploadVersion(ctx, "alice", tmpl.ID, UploadParams{File: []byte(`<h2>{{.title}}</h2>`), Schema: testSchema})
	if err != nil {
		t.Fatalf("replace draft: %v", err)
	}
	if created || second.ID != first.ID {
		t.Fatalf("want the same draft replaced, got created=%v", created)
	}
	if second.FileKey == first.FileKey {
		t.Fatalf("file key did not change: %s", second.FileKey)
	}

	// Новый файл на месте, старый удалён: на него больше никто не ссылается.
	r, err := svc.files.Get(ctx, svc.bucket, second.FileKey)
	if err != nil {
		t.Fatalf("get new file: %v", err)
	}
	_ = r.Close()
	if _, err := svc.files.Get(ctx, svc.bucket, first.FileKey); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("old file: err = %v, want ErrNotFound", err)
	}
}

func TestResolveLocaleInput(t *testing.T) {
	svc, _, project := newTestService(t)
	ctx := t.Context()

	p := validParams(project)
	p.Locale = "kk"
	tmpl, err := svc.Create(ctx, "alice", p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	publishActive(t, svc, tmpl.ID, string(testSchema))

	got, err := svc.Resolve(ctx, project, Ref{Code: p.Code, Locale: "kz"})
	if err != nil || got.Locale != "kk" {
		t.Errorf("legacy kz: got %+v, %v; want kk", got, err)
	}

	var vErr *ValidationError
	if _, err := svc.Resolve(ctx, project, Ref{Code: p.Code, Locale: "en"}); !errors.As(err, &vErr) || vErr.Field != "locale" {
		t.Errorf("unknown locale: err = %v, want ValidationError on locale", err)
	}
}
