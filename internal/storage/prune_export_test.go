package storage

// ExportPruneSamplesSQL is one lane's prune statements by owner arc, for the
// external test that plans each and holds its probe to an index (#417).
func ExportPruneSamplesSQL(lane string) map[string]string {
	out := map[string]string{}
	for _, arc := range sampleArcs {
		out[arc] = pruneSamplesSQL(lane, arc)
	}
	return out
}

// ExportPruneLogsSQL is one log table's prune statements, "rated" (by the
// levels in $3) and "unrated", for the external access-path test (#417).
func ExportPruneLogsSQL(table string) map[string]string {
	return map[string]string{"rated": pruneLogsSQL(table, "rated"), "unrated": pruneLogsSQL(table, "unrated")}
}
