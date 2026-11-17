package templates

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"docgen/internal/db"
)

func TestUploadVersionExpectedDraft(t *testing.T) {
	svc, _, project := newTestService(t)
	ctx := t.Context()

	tmpl, err := svc.Create(ctx, "alice", validParams(project))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	upload := func(expect *DraftExpectation, title string) error {
		_, _, err := svc.UploadVersion(ctx, "alice", tmpl.ID, UploadParams{
			File: []byte("<h1>" + title + "</h1>"), Schema: testSchema, Expect: expect,
		})
		return err
	}

	// Черновика нет и клиент его не ждёт — создаётся.
	if err := upload(&DraftExpectation{}, "v1"); err != nil {
		t.Fatalf("first upload: %v", err)
	}
	// Кто-то другой уже создал черновик, а клиент открывал шаблон без него.
	if err := upload(&DraftExpectation{}, "other"); !errors.Is(err, ErrDraftChanged) {
		t.Errorf("draft appeared: err = %v, want ErrDraftChanged", err)
	}

	draft := mustDraft(t, svc, tmpl.ID)
	stale := draft.UpdatedAt.Add(-time.Second)
	if err := upload(&DraftExpectation{UpdatedAt: &stale}, "stale"); !errors.Is(err, ErrDraftChanged) {
		t.Errorf("stale draft: err = %v, want ErrDraftChanged", err)
	}
	if err := upload(&DraftExpectation{UpdatedAt: &draft.UpdatedAt}, "fresh"); err != nil {
		t.Errorf("fresh draft: %v", err)
	}
	// Без ожидания — прежнее поведение: черновик просто заменяется.
	if err := upload(nil, "no check"); err != nil {
		t.Errorf("no expectation: %v", err)
	}

	// Черновик опубликовали — тот, кто его правил, не должен создать новый молча.
	draft = mustDraft(t, svc, tmpl.ID)
	if _, err := svc.Publish(ctx, "bob", draft.ID, PublishOptions{}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := upload(&DraftExpectation{UpdatedAt: &draft.UpdatedAt}, "after publish"); !errors.Is(err, ErrDraftChanged) {
		t.Errorf("published draft: err = %v, want ErrDraftChanged", err)
	}
	if err := upload(&DraftExpectation{}, "v2"); err != nil {
		t.Errorf("new draft after publish: %v", err)
	}
}

func mustDraft(t *testing.T, svc *Service, templateID uuid.UUID) db.TemplateVersion {
	t.Helper()
	draft, err := db.New(svc.pool).GetDraftVersion(t.Context(), templateID)
	if err != nil {
		t.Fatalf("GetDraftVersion: %v", err)
	}
	return draft
}
