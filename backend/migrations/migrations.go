// Package migrations содержит SQL-миграции схемы БД.
package migrations

import "embed"

// FS содержит все файлы миграций, встроенные в бинарник.
//
//go:embed *.sql
var FS embed.FS
