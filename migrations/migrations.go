package migrations

import _ "embed"

//go:embed 001_init.sql
var initialSQL string

//go:embed 002_identities.sql
var identitiesSQL string

var SQL = initialSQL + "\n" + identitiesSQL
