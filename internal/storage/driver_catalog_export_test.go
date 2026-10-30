package storage

import "context"

// DriverCatalogLanes exposes the emit lane each name resolves to in the catalog a
// driver spec is validated against, for the tests that hold it to the ingest
// registry's resolution (#1307).
func (p *PG) DriverCatalogLanes(ctx context.Context) (map[string]string, error) {
	cat, err := loadDriverCatalog(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	return cat.lanes, nil
}
