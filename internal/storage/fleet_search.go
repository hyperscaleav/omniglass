package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// SearchMaxLimit caps the per-kind result count of a fleet search, whatever the
// caller asks for: the palette shows a handful per group, never a collection.
const SearchMaxLimit = 20

// FleetSearchResult is the hits for one search text, one slice per tree kind,
// each already capped and ordered best match first.
type FleetSearchResult struct {
	Locations  []Location
	Systems    []System
	Components []Component
	// SystemPlaces is the label (the name where it has none) of the location
	// each system hit sits at, by location id: the address a system is named
	// with (#2757). Read through the location scope, so a location the caller
	// cannot read is not in it.
	SystemPlaces map[string]string
}

// likePattern turns typed text into a LIKE pattern matching it anywhere, with
// the wildcard characters in the text taken literally.
func likePattern(text string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(strings.TrimSpace(text)) + "%"
}

// SearchFleet finds locations, systems and components whose name or label holds
// text (case-insensitive, anywhere), at most limit of each kind, each kind read
// through its own scope. Scope is injected by scopedListSQL, the same rooted
// walk every list uses, so a hit outside the caller's read scope cannot appear
// and the count is bounded in the database, never trimmed in Go. Best match
// first: an exact name or label, then a prefix, then the rest by name.
func (p *PG) SearchFleet(ctx context.Context, text string, limit int, locRead, sysRead, compRead scope.Set) (*FleetSearchResult, error) {
	out := &FleetSearchResult{}
	if strings.TrimSpace(text) == "" {
		return out, nil
	}
	if limit <= 0 || limit > SearchMaxLimit {
		limit = SearchMaxLimit
	}
	var err error
	if out.Locations, err = scopedSearch(ctx, p, locationConfig, locRead, text, limit); err != nil {
		return nil, err
	}
	if out.Systems, err = scopedSearch(ctx, p, systemConfig, sysRead, text, limit); err != nil {
		return nil, err
	}
	if out.Components, err = scopedSearch(ctx, p, componentConfig, compRead, text, limit); err != nil {
		return nil, err
	}
	if out.SystemPlaces, err = p.searchSystemPlaces(ctx, out.Systems, locRead); err != nil {
		return nil, err
	}
	return out, nil
}

// searchSystemPlaces reads the label of each location the system hits sit at,
// through the caller's location read scope.
func (p *PG) searchSystemPlaces(ctx context.Context, systems []System, read scope.Set) (map[string]string, error) {
	var ids []string
	seen := map[string]bool{}
	for _, s := range systems {
		if s.LocationID != nil && !seen[*s.LocationID] {
			seen[*s.LocationID] = true
			ids = append(ids, *s.LocationID)
		}
	}
	if len(ids) == 0 || read.Empty() {
		return nil, nil
	}
	args, ok := scopedListArgs(locationTable, read)
	if !ok {
		return nil, nil
	}
	args = append(args, ids)
	rows, err := p.pool.Query(ctx, fmt.Sprintf(
		`select v.id::text, coalesce(nullif(btrim(v.label), ''), v.name) from (%s) v where v.id = any($%d::uuid[])`,
		scopedListSQL(locationTable, "id, name, label", read), len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: read the locations of system hits: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, label string
		if err := rows.Scan(&id, &label); err != nil {
			return nil, fmt.Errorf("storage: scan a system hit's location: %w", err)
		}
		out[id] = label
	}
	return out, rows.Err()
}

// scopedSearch is scopedList narrowed to a name search and a limit: the ids are
// picked inside the database over the scoped list, then the winners are read
// whole (with their addresses) in rank order.
func scopedSearch[T any](ctx context.Context, p *PG, cfg scopedConfig[T], read scope.Set, text string, limit int) ([]T, error) {
	if read.Empty() {
		return nil, nil
	}
	args, ok := scopedListArgs(cfg.table, read)
	if !ok {
		return nil, nil
	}
	n := len(args)
	args = append(args, likePattern(text), strings.TrimSpace(text), limit)
	like, exact, lim := n+1, n+2, n+3
	prefix := fmt.Sprintf(`coalesce(lower(left(v.name, char_length($%[1]d))) = lower($%[1]d) or lower(left(v.label, char_length($%[1]d))) = lower($%[1]d), false)`, exact)
	rows, err := p.pool.Query(ctx, fmt.Sprintf(
		`select v.id::text from (%s) v
		 where v.name ilike $%d escape '\' or v.label ilike $%d escape '\'
		 order by coalesce(lower(v.name) = lower($%d) or lower(v.label) = lower($%d), false) desc, (%s) desc, v.name, v.id
		 limit $%d`,
		scopedListSQL(cfg.table, "id, name, label", read), like, like, exact, exact, prefix, lim), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: search %s: %w", cfg.table, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: scan %s search: %w", cfg.table, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	full, err := p.pool.Query(ctx, `select `+cfg.cols+` from `+string(cfg.table)+` where id = any($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("storage: read %s search hits: %w", cfg.table, err)
	}
	defer full.Close()
	byID := make(map[string]*T, len(ids))
	for full.Next() {
		v, err := cfg.scan(full)
		if err != nil {
			return nil, fmt.Errorf("storage: scan %s hit: %w", cfg.table, err)
		}
		byID[cfg.idOf(v)] = v
	}
	if err := full.Err(); err != nil {
		return nil, err
	}
	out := make([]T, 0, len(ids))
	for _, id := range ids {
		if v, ok := byID[id]; ok {
			out = append(out, *v)
		}
	}
	if cfg.attachPaths != nil && len(out) > 0 {
		vs := make([]*T, len(out))
		for i := range out {
			vs[i] = &out[i]
		}
		if err := cfg.attachPaths(ctx, p.pool, vs, false); err != nil {
			return nil, err
		}
	}
	return out, nil
}
