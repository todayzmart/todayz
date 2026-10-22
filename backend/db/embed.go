// Package dbschema embeds the goose SQL migrations so the binary carries them.
package dbschema

import "embed"

// Migrations holds migrations/*.sql.
//
//go:embed migrations/*.sql
var Migrations embed.FS
