package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The three catalog list statements the ingest sample registry is built from
// (ListMetricTypes, ListPropertyTypes, ListEventTypes), named so the
// fingerprint's drift guard can read the tables they touch.
var (
	metricTypeListSQL   = `select ` + metricTypeCols + ` from metric_type`
	propertyTypeListSQL = `select ` + propertyCols + ` from property_type`
	eventTypeListSQL    = `select id, name, coalesce(label, ''), description, payload_schema, official from event_type`
)

// registryTables is every table the registry list statements read, the three
// catalogs and the calc rules a metric or property type's writer comes from.
// TestCatalogFingerprintCoversTheRegistryReads fails when a list statement
// reads a table missing here.
var registryTables = []string{"metric_type", "property_type", "event_type", "calc_rule"}

// catalogFingerprintSQL folds every row version (xmin) of every registry
// table, in id order, into one SHA-256 digest (not md5, which a FIPS-mode
// Postgres refuses). An insert, an update or a delete in any
// of them changes it, because each writes a row version with its own
// transaction id or removes one; nothing else does (a freeze keeps xmin), so
// a match means the registry built from the lists is still current.
var catalogFingerprintSQL = func() string {
	parts := make([]string, len(registryTables))
	for i, t := range registryTables {
		parts[i] = fmt.Sprintf(`coalesce((select string_agg(xmin::text, ',' order by id) from %s), '')`, pgx.Identifier{t}.Sanitize())
	}
	return `select encode(sha256(convert_to(` + strings.Join(parts, ` || '|' || `) + `, 'UTF8')), 'hex')`
}()

// CatalogFingerprint is a digest of the rows the ingest sample registry is
// built from (#1279). The consumer keeps a registry between batches and
// rebuilds it only when this changes, so a batch reads one short value instead
// of three catalogs, and a catalog change is still honored by the next batch.
func (p *PG) CatalogFingerprint(ctx context.Context) (string, error) {
	var fp string
	if err := p.pool.QueryRow(ctx, catalogFingerprintSQL).Scan(&fp); err != nil {
		return "", fmt.Errorf("storage: catalog fingerprint: %w", err)
	}
	return fp, nil
}
