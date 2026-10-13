package migrations

import _ "embed"

//go:embed 001_init.sql
var initialSQL string

//go:embed 002_identities.sql
var identitiesSQL string

//go:embed 003_sites.sql
var sitesSQL string

//go:embed 004_resource_quotas.sql
var quotasSQL string

const Count = 4

var SQL = initialSQL + "\n" + identitiesSQL + "\n" + sitesSQL + "\n" + quotasSQL
