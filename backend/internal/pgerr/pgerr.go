// Package pgerr распознаёт типовые ошибки PostgreSQL по их кодам.
package pgerr

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// Коды ошибок PostgreSQL: https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
)

// IsUniqueViolation сообщает, что запись нарушила UNIQUE (такая уже есть).
func IsUniqueViolation(err error) bool {
	return hasCode(err, codeUniqueViolation)
}

// IsForeignKeyViolation сообщает, что запись ссылается на несуществующую строку.
func IsForeignKeyViolation(err error) bool {
	return hasCode(err, codeForeignKeyViolation)
}

func hasCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
