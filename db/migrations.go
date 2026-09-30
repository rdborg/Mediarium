// Package db embeds the SQL migration files in this directory so the
// compiled binary carries its own schema (/db/ holds
// SQLite schema/migrations; this file is what makes them buildable into
// the single static binary rather than read from disk at runtime).
package db

import "embed"

//go:embed migrations/*.sql
var MigrationsFS embed.FS
