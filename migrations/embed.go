// Package migrations embeds the SQL migration files.
//
// Embedding them means the production binary carries its own schema history:
// the image needs no migrations directory mounted, and `db-deploy` can run from
// the same artifact that serves traffic.
package migrations

import "embed"

// FS holds every .sql migration in this directory.
//
//go:embed *.sql
var FS embed.FS
