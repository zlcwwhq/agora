// Package migrations holds the local index's schema, the single source for it.
package migrations

import _ "embed"

// Index creates the index schema in an empty database. It sets user_version to
// IndexVersion.
//
//go:embed index.sql
var Index string

// IndexVersion is the user_version that Index sets. An index with any other
// version is deleted and rebuilt.
const IndexVersion = 2
