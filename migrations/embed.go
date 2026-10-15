// Package migrations embarque les migrations goose dans le binaire.
package migrations

import "embed"

// FS contient les fichiers *.sql des migrations.
//
//go:embed *.sql
var FS embed.FS
