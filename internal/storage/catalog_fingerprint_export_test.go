package storage

// The registry list statements and the tables the catalog fingerprint covers,
// for the external integration test that plans each statement and holds every
// relation it reads to the covered set (#1279).
var (
	ExportRegistryListSQL = registryListSQL
	ExportRegistryTables  = registryTables
)
