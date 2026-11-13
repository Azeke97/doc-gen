package templates

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"docgen/internal/db"
)

// Publish замораживает черновик версии. После публикации версию нельзя изменить
// ни через сервис, ни в базе напрямую (триггер template_versions_immutable).
func (s *Service) Publish(ctx context.Context, actor string, versionID uuid.UUID) (db.TemplateVersion, error) {
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

		version, err = q.PublishVersion(ctx, db.PublishVersionParams{ID: versionID, PublishedBy: &actor})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVersionAlreadyPublished
		}
		if err != nil {
			return err
		}

		return writeAudit(ctx, q, actor, ActionVersionPublished, entityTemplateVersion, version.ID, map[string]any{
			"template_id": version.TemplateID,
			"version":     version.Version,
		})
	})
	if err != nil {
		return db.TemplateVersion{}, fmt.Errorf("publish version: %w", err)
	}

	return version, nil
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
