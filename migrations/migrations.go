// Package migrations holds the board's database schema, the single source for
// it, and the upgrades of databases that earlier releases made.
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
const Version = 4

//go:embed upgrade-2.sql
var upgrade2 string

//go:embed upgrade-3.sql
var upgrade3 string

// Upgrades holds, for each version from 2 to Version-1, the statements that
// bring a database of that version to the next one, setting its
// user_version. A database of version 2 reaches Version through each in turn.
var Upgrades = map[int]string{
	2: upgrade2,
	3: upgrade3,
}
