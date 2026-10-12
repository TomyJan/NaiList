package migrations

import _ "embed"

//go:embed 001_init.sql
var SQL string
