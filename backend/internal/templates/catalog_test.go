package templates

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"docgen/internal/db"
)

// TestPortalCatalog проверяет запрос каталога портала на настоящей базе:
// видны только активные шаблоны своего проекта с номером активной версии.
func TestPortalCatalog(t *testing.T) {
	svc, _, project := newTestService(t)
	ctx := t.Context()

	// opening_protocol: v1 и v2 опубликованы, активна v2.
	// Черновик у шаблона один, поэтому v2 загружаем только после публикации v1.
	opening, v1 := uploadDraft(t, svc, project, uuid.Nil)
	if _, err := svc.Publish(ctx, "bob", v1.ID, PublishOptions{}); err != nil {
		t.Fatalf("Publish v1: %v", err)
	}
	_, v2 := uploadDraft(t, svc, project, opening.ID)
	if _, err := svc.Publish(ctx, "bob", v2.ID, PublishOptions{}); err != nil {
		t.Fatalf("Publish v2: %v", err)
	}
	if _, err := svc.Activate(ctx, "bob", opening.ID, v2.ID, ActivateOptions{}); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	// result_protocol: только черновик — порталу не виден.
	p := validParams(project)
	p.Code = "result_protocol"
	if _, err := svc.Create(ctx, "alice", p); err != nil {
		t.Fatalf("Create draft: %v", err)
	}

	got, err := svc.ListActive(ctx, project)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	want := []db.ListActiveTemplatesByProjectRow{
		{Code: "opening_protocol", Locale: "bilingual", Name: "Протокол вскрытия", Engine: "html", ActiveVersion: 2},
	}
	if !slices.Equal(got, want) {
		t.Errorf("catalog = %+v, want %+v", got, want)
	}

	// После архивации шаблон из каталога пропадает.
	if _, err := svc.Archive(ctx, "bob", opening.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	got, err = svc.ListActive(ctx, project)
	if err != nil {
		t.Fatalf("ListActive after archive: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("catalog after archive = %+v, want empty", got)
	}

	// Чужой проект наших шаблонов не видит.
	other, err := svc.ListActive(ctx, project+"_other")
	if err != nil {
		t.Fatalf("ListActive other: %v", err)
	}
	if len(other) != 0 {
		t.Errorf("other project catalog = %+v, want empty", other)
	}
}
