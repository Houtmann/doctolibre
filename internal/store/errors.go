package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	codeUniqueViolation    = "23505"
	codeExclusionViolation = "23P01"
)

func pgCode(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code, true
	}
	return "", false
}

// IsExclusionViolation indique une violation de contrainte EXCLUDE (SQLSTATE 23P01).
func IsExclusionViolation(err error) bool {
	c, ok := pgCode(err)
	return ok && c == codeExclusionViolation
}

// IsUniqueViolation indique une violation de contrainte UNIQUE (SQLSTATE 23505).
func IsUniqueViolation(err error) bool {
	c, ok := pgCode(err)
	return ok && c == codeUniqueViolation
}

// constraintName retourne le nom de la contrainte violée, "" si absent.
func constraintName(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}
