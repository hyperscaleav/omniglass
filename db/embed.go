// Package db embeds the dbmate migration set so the single binary is
// self-contained: `omniglass migrate` applies the schema with no filesystem
// dependency at runtime, and tests migrate ephemeral databases from the same
// embedded source.
package db

import "embed"

// FS holds the dbmate migration files. dbmate is pointed at it via
// mate.FS in internal/migrate; the "migrations" subdirectory is the
// MigrationsDir.
//
//go:embed migrations/*.sql
var FS embed.FS
