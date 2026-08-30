// Package migrations embeds the coordinator's SQL schema migrations.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
