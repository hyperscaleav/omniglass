package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrCatalogKeyNotFound is a catalog key no row holds: no row of that name in
// the namespace, or a uuid whose row lives in another namespace (#1434).
var ErrCatalogKeyNotFound = errors.New("storage: no catalog row holds that namespace and key")

// keyedCatalogs are the catalogs a route addresses as {namespace}/{key}
// (ADR-0318): the 18 that carry official, each with a namespace column.
var keyedCatalogs = map[string]bool{
	"metric_type": true, "property_type": true, "event_type": true, "command_type": true,
	"component_type": true, "system_type": true, "location_type": true, "secret_type": true,
	"severity_level": true, "notification_type": true, "event_rule": true, "action_rule": true,
	"calc_rule": true, "vendor": true, "product": true, "driver": true, "standard": true,
	"role": true,
}

// ResolveCatalogKey turns a catalog address, a namespace and a key, into the
// ref the catalog's own reads take for exactly that row (#1434): its name when
// the search path reaches that row by its name, so every by-name read lands on
// it, else its uuid (ADR-0062), the one ref that reaches a row another
// namespace's row of the same name shadows. The key is a name, read in that
// namespace only, or a uuid, which answers only when its row lives in the
// namespace named. No such row is ErrCatalogKeyNotFound. The API edge calls
// it once per address, so nothing below it parses a joined string.
func (p *PG) ResolveCatalogKey(ctx context.Context, table, namespace, key string) (string, error) {
	if !keyedCatalogs[table] {
		return "", fmt.Errorf("storage: %s is not a namespaced catalog", table)
	}
	reached := "true"
	if namespacedCatalogs[table] {
		reached = `exists (select 1 from ` + table + `_in_effect() e where e.id = t.id)`
	}
	var id, name string
	var inEffect bool
	err := p.pool.QueryRow(ctx, `select t.id::text, t.name, `+reached+` from `+table+` t
		where t.namespace = $1 and t.`+catalogRefCol(key)+` = $2`, namespace, key).Scan(&id, &name, &inEffect)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrCatalogKeyNotFound
	}
	if err != nil {
		return "", fmt.Errorf("storage: resolve %s %s/%s: %w", table, namespace, key, err)
	}
	if inEffect {
		return name, nil
	}
	return id, nil
}
