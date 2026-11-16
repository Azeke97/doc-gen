package templates

import (
	"context"
	"fmt"
	"strings"

	"docgen/internal/db"
)

// Ref — как портал называет шаблон: код, локаль и (необязательно) номер версии.
// Пустая локаль допустима, если у кода одна локаль. Version = 0 — активная версия.
type Ref struct {
	Code    string
	Locale  string
	Version int32
}

// ListActive возвращает каталог для портала: активные шаблоны проекта.
func (s *Service) ListActive(ctx context.Context, project string) ([]db.ListActiveTemplatesByProjectRow, error) {
	list, err := db.New(s.pool).ListActiveTemplatesByProject(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("list active templates: %w", err)
	}

	return list, nil
}

// Resolve находит версию шаблона, которую портал имеет в виду. Шаблоны чужих проектов,
// черновики и архив не находятся вовсе — для портала их не существует.
// Тот же поиск понадобится генерации (этап 4): документ выпускается по найденной версии.
func (s *Service) Resolve(ctx context.Context, project string, ref Ref) (db.ResolveTemplateVersionRow, error) {
	params := db.ResolveTemplateVersionParams{Project: project, Code: ref.Code}
	if ref.Locale != "" {
		params.Locale = &ref.Locale
	}
	if ref.Version != 0 {
		params.Version = &ref.Version
	}

	rows, err := db.New(s.pool).ResolveTemplateVersion(ctx, params)
	if err != nil {
		return db.ResolveTemplateVersionRow{}, fmt.Errorf("resolve template: %w", err)
	}

	switch {
	case len(rows) == 1:
		return rows[0], nil
	case len(rows) > 1:
		locales := make([]string, 0, len(rows))
		for _, r := range rows {
			locales = append(locales, r.Locale)
		}
		return db.ResolveTemplateVersionRow{}, &ValidationError{
			Field:   "locale",
			Message: "template " + ref.Code + " has several locales, choose one of: " + strings.Join(locales, ", "),
		}
	case ref.Version != 0:
		return db.ResolveTemplateVersionRow{}, ErrVersionNotFound
	default:
		return db.ResolveTemplateVersionRow{}, ErrTemplateNotFound
	}
}
