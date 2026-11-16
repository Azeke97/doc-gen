package templates

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"docgen/internal/dataschema"
	"docgen/internal/db"
)

// PublishOptions — настройки публикации.
type PublishOptions struct {
	// ConfirmBreaking — администратор видел ломающие изменения схемы и публикует осознанно.
	ConfirmBreaking bool
}

// BreakingChangesError — схема новой версии несовместима с базовой (ТЗ, Ф2):
// публикация остановлена, пока администратор не подтвердит её.
type BreakingChangesError struct {
	Diff VersionDiff
}

func (e *BreakingChangesError) Error() string {
	return "schema has breaking changes compared to the version portals use now; confirm to publish"
}

// VersionDiff — чем схема версии отличается от базовой. BaseVersion — номер версии,
// с которой сравнивали; nil — сравнивать не с чем (это первая публикация).
type VersionDiff struct {
	BaseVersion *int32
	Changes     []dataschema.Change
}

// Publish замораживает черновик версии. После публикации версию нельзя изменить
// ни через сервис, ни в базе напрямую (триггер template_versions_immutable).
// Если схема ломает совместимость с базовой версией, нужен opts.ConfirmBreaking.
func (s *Service) Publish(ctx context.Context, actor string, versionID uuid.UUID, opts PublishOptions) (db.TemplateVersion, error) {
	var version db.TemplateVersion

	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)

		draft, err := q.GetTemplateVersion(ctx, versionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVersionNotFound
		}
		if err != nil {
			return err
		}

		// Блокируем шаблон, как и при загрузке: так публикация и загрузка новой версии
		// одного шаблона не пересекутся, и блокировки всегда берутся в одном порядке.
		tmpl, err := q.LockTemplate(ctx, draft.TemplateID)
		if err != nil {
			return err
		}
		if tmpl.Status == "archived" {
			return ErrTemplateArchived
		}
		if draft.Status != "draft" {
			return ErrVersionAlreadyPublished
		}

		diff, err := diffWithBase(ctx, q, tmpl, draft)
		if err != nil {
			return err
		}
		breaking := dataschema.HasBreaking(diff.Changes)
		if breaking && !opts.ConfirmBreaking {
			return &BreakingChangesError{Diff: diff}
		}

		version, err = q.PublishVersion(ctx, db.PublishVersionParams{ID: versionID, PublishedBy: &actor})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVersionAlreadyPublished
		}
		if err != nil {
			return err
		}

		return writeAudit(ctx, q, actor, ActionVersionPublished, entityTemplateVersion, version.ID, map[string]any{
			"template_id":        version.TemplateID,
			"version":            version.Version,
			"base_version":       diff.BaseVersion,
			"schema_changes":     diff.Changes,
			"breaking_confirmed": breaking,
		})
	})
	if err != nil {
		return db.TemplateVersion{}, fmt.Errorf("publish version: %w", err)
	}

	return version, nil
}

// DiffVersion показывает, чем схема версии отличается от базовой: так админка
// предупреждает о ломающих изменениях ещё до нажатия «Опубликовать» (ТЗ, Ф2, Ф14).
func (s *Service) DiffVersion(ctx context.Context, versionID uuid.UUID) (VersionDiff, error) {
	q := db.New(s.pool)

	version, err := q.GetTemplateVersion(ctx, versionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return VersionDiff{}, ErrVersionNotFound
	}
	if err != nil {
		return VersionDiff{}, fmt.Errorf("get version: %w", err)
	}

	tmpl, err := q.GetTemplate(ctx, version.TemplateID)
	if err != nil {
		return VersionDiff{}, fmt.Errorf("get template: %w", err)
	}

	diff, err := diffWithBase(ctx, q, tmpl, version)
	if err != nil {
		return VersionDiff{}, fmt.Errorf("diff version: %w", err)
	}

	return diff, nil
}

// diffWithBase сравнивает схему версии с базовой. База — то, по чему порталы
// генерируют сейчас: активная версия шаблона. Если активной нет (или это она сама) —
// предыдущая опубликованная версия.
func diffWithBase(ctx context.Context, q *db.Queries, tmpl db.Template, v db.TemplateVersion) (VersionDiff, error) {
	var (
		base db.TemplateVersion
		err  error
	)
	if tmpl.ActiveVersionID != nil && *tmpl.ActiveVersionID != v.ID {
		base, err = q.GetTemplateVersion(ctx, *tmpl.ActiveVersionID)
	} else {
		base, err = q.GetPreviousPublishedVersion(ctx, db.GetPreviousPublishedVersionParams{
			TemplateID: v.TemplateID,
			Version:    v.Version,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return VersionDiff{Changes: []dataschema.Change{}}, nil
	}
	if err != nil {
		return VersionDiff{}, err
	}

	changes, err := dataschema.Diff(base.DataSchema, v.DataSchema)
	if err != nil {
		return VersionDiff{}, err
	}
	if changes == nil {
		changes = []dataschema.Change{}
	}

	return VersionDiff{BaseVersion: &base.Version, Changes: changes}, nil
}

// Activate делает опубликованную версию версией по умолчанию и переводит шаблон в active.
// Им же выполняется откат: активировать предыдущую версию.
func (s *Service) Activate(ctx context.Context, actor string, templateID, versionID uuid.UUID) (db.Template, error) {
	var tmpl db.Template

	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)

		locked, err := q.LockTemplate(ctx, templateID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTemplateNotFound
		}
		if err != nil {
			return err
		}
		if locked.Status == "archived" {
			return ErrTemplateArchived
		}

		version, err := q.GetTemplateVersion(ctx, versionID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && version.TemplateID != templateID) {
			return ErrVersionNotFound
		}
		if err != nil {
			return err
		}
		if version.Status != "published" {
			return ErrVersionNotPublished
		}

		tmpl, err = q.SetActiveVersion(ctx, db.SetActiveVersionParams{ID: templateID, ActiveVersionID: &versionID})
		if err != nil {
			return err
		}

		return writeAudit(ctx, q, actor, ActionTemplateActivated, entityTemplate, templateID, map[string]any{
			"version_id":          versionID,
			"version":             version.Version,
			"previous_version_id": locked.ActiveVersionID,
		})
	})
	if err != nil {
		return db.Template{}, fmt.Errorf("activate version: %w", err)
	}

	return tmpl, nil
}

// Archive выводит шаблон из обращения: новые версии и активация больше невозможны.
// Уже выпущенные документы и их версии остаются.
func (s *Service) Archive(ctx context.Context, actor string, templateID uuid.UUID) (db.Template, error) {
	var tmpl db.Template

	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)

		locked, err := q.LockTemplate(ctx, templateID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTemplateNotFound
		}
		if err != nil {
			return err
		}
		if locked.Status == "archived" {
			return ErrTemplateArchived
		}

		if tmpl, err = q.ArchiveTemplate(ctx, templateID); err != nil {
			return err
		}

		return writeAudit(ctx, q, actor, ActionTemplateArchived, entityTemplate, templateID, map[string]any{
			"previous_status": locked.Status,
		})
	})
	if err != nil {
		return db.Template{}, fmt.Errorf("archive template: %w", err)
	}

	return tmpl, nil
}
