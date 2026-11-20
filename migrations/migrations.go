// Package migrations holds the board's database schema, the single source for
// it.
package migrations

import _ "embed"

// Schema creates the board's schema in an empty database. It sets
// user_version to Version.
//
//go:embed schema.sql
var Schema string

// Version is the user_version that Schema sets. A database with a higher
// version comes from a later agora, and is refused, as is one with version
// 1, which only development builds made, before boards.
const Version = 2
