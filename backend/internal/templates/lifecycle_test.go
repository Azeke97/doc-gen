package templates

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"docgen/internal/db"
)

// uploadDraft создаёт шаблон (если tmplID пустой) и загружает в него черновик.
func uploadDraft(t *testing.T, svc *Service, project string, tmplID uuid.UUID) (db.Template, db.TemplateVersion) {
	t.Helper()
	ctx := t.Context()

	var tmpl db.Template
	if tmplID == uuid.Nil {
		var err error
		if tmpl, err = svc.Create(ctx, "alice", validParams(project)); err != nil {
			t.Fatalf("Create: %v", err)
		}
		tmplID = tmpl.ID
	}

	version, _, err := svc.UploadVersion(ctx, "alice", tmplID, UploadParams{File: testHTML, Schema: testSchema})
	if err != nil {
		t.Fatalf("UploadVersion: %v", err)
	}

	return tmpl, version
}

func TestTemplateLifecycle(t *testing.T) {
	svc, tx, project := newTestService(t)
	ctx := t.Context()

	tmpl, v1 := uploadDraft(t, svc, project, uuid.Nil)

	// Черновик нельзя сделать активным.
	if _, err := svc.Activate(ctx, "bob", tmpl.ID, v1.ID, ActivateOptions{}); !errors.Is(err, ErrVersionNotPublished) {
		t.Fatalf("activate draft: err = %v, want ErrVersionNotPublished", err)
	}

	published, err := svc.Publish(ctx, "bob", v1.ID, PublishOptions{})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if published.Status != "published" || published.PublishedBy == nil || *published.PublishedBy != "bob" || published.PublishedAt == nil {
		t.Errorf("published = %+v, want status published by bob with time", published)
	}

	if _, err := svc.Publish(ctx, "bob", v1.ID, PublishOptions{}); !errors.Is(err, ErrVersionAlreadyPublished) {
		t.Errorf("publish twice: err = %v, want ErrVersionAlreadyPublished", err)
	}

	active, err := svc.Activate(ctx, "bob", tmpl.ID, v1.ID, ActivateOptions{})
	if err != nil {
		t.Fatalf("Activate v1: %v", err)
	}
	if active.Status != "active" || active.ActiveVersionID == nil || *active.ActiveVersionID != v1.ID {
		t.Errorf("after activate: status %s, active %v; want active, %s", active.Status, active.ActiveVersionID, v1.ID)
	}

	// После публикации v1 можно загрузить v2, опубликовать, активировать и откатиться на v1.
	_, v2 := uploadDraft(t, svc, project, tmpl.ID)
	if v2.Version != 2 {
		t.Fatalf("second version number = %d, want 2", v2.Version)
	}
	if _, err := svc.Publish(ctx, "bob", v2.ID, PublishOptions{}); err != nil {
		t.Fatalf("Publish v2: %v", err)
	}
	if _, err := svc.Activate(ctx, "bob", tmpl.ID, v2.ID, ActivateOptions{}); err != nil {
		t.Fatalf("Activate v2: %v", err)
	}
	rolledBack, err := svc.Activate(ctx, "bob", tmpl.ID, v1.ID, ActivateOptions{})
	if err != nil {
		t.Fatalf("roll back to v1: %v", err)
	}
	if *rolledBack.ActiveVersionID != v1.ID {
		t.Errorf("after rollback active = %s, want %s", *rolledBack.ActiveVersionID, v1.ID)
	}

	archived, err := svc.Archive(ctx, "bob", tmpl.ID)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if archived.Status != "archived" {
		t.Errorf("status = %s, want archived", archived.Status)
	}
	if _, err := svc.Archive(ctx, "bob", tmpl.ID); !errors.Is(err, ErrTemplateArchived) {
		t.Errorf("archive twice: err = %v, want ErrTemplateArchived", err)
	}
	if _, err := svc.Activate(ctx, "bob", tmpl.ID, v2.ID, ActivateOptions{}); !errors.Is(err, ErrTemplateArchived) {
		t.Errorf("activate archived: err = %v, want ErrTemplateArchived", err)
	}

	// Каждое действие — запись в журнале.
	var actions []string
	rows, err := tx.Query(ctx, `SELECT action FROM audit_log
		WHERE entity_id = $1 OR entity_id IN (SELECT id FROM template_versions WHERE template_id = $1)
		ORDER BY occurred_at, id`, tmpl.ID)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatalf("scan: %v", err)
		}
		actions = append(actions, a)
	}
	want := []string{
		ActionTemplateCreated,
		ActionVersionUploaded, ActionVersionPublished, ActionTemplateActivated,
		ActionVersionUploaded, ActionVersionPublished, ActionTemplateActivated, ActionTemplateActivated,
		ActionTemplateArchived,
	}
	if len(actions) != len(want) {
		t.Fatalf("audit actions = %v, want %v", actions, want)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Errorf("audit[%d] = %s, want %s", i, actions[i], want[i])
		}
	}
}

func TestActivateForeignVersion(t *testing.T) {
	svc, _, project := newTestService(t)
	ctx := t.Context()

	p := validParams(project)
	first, err := svc.Create(ctx, "alice", p)
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	p.Code = "result_protocol"
	second, err := svc.Create(ctx, "alice", p)
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	_, foreign := uploadDraft(t, svc, project, second.ID)
	if _, err := svc.Publish(ctx, "bob", foreign.ID, PublishOptions{}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Версия принадлежит другому шаблону — для первого шаблона её «нет».
	if _, err := svc.Activate(ctx, "bob", first.ID, foreign.ID, ActivateOptions{}); !errors.Is(err, ErrVersionNotFound) {
		t.Errorf("err = %v, want ErrVersionNotFound", err)
	}
	if _, err := svc.Publish(ctx, "bob", uuid.New(), PublishOptions{}); !errors.Is(err, ErrVersionNotFound) {
		t.Errorf("publish unknown: err = %v, want ErrVersionNotFound", err)
	}
	if _, err := svc.Archive(ctx, "bob", uuid.New()); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("archive unknown: err = %v, want ErrTemplateNotFound", err)
	}
}
