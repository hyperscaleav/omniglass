package storage

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The two namespaces a bare name searches (#318, ADR-0318): the operator's
// own, then the platform's.
const (
	NamespaceLocal = "local"
	NamespaceOG    = "og"
)

// NameHolder is one catalog row holding a name: which catalog ("metric",
// "property" or "event") and which namespace.
type NameHolder struct {
	Lane      string
	Namespace string
}

// ResolveNameLane is the one rule for which row a bare name resolves to (#1307,
// #1432): the search path, shared by the ingest registry and every reader of a
// name (an event rule's sample key, a calc rule's key, a driver's emit). The
// three ingest catalogs share one name space, so the path runs across them:
//   - the operator's local holders answer first: exactly one resolves, and
//     shadowed is true when an og row holds the name too;
//   - with no local holder, exactly one og holder resolves;
//   - two or more holders in the answering tier resolve to nothing (a
//     collision): picking one would silently make the other unwritable;
//   - a reserved key, the node's or the platform's own, is never shadowed:
//     held in both tiers, it resolves to nothing;
//   - a holder in any other namespace never answers a bare name.
//
// ok is false for a name nothing holds too.
func ResolveNameLane(name string, holders []NameHolder) (winner NameHolder, shadowed, ok bool) {
	var local, og []NameHolder
	for _, h := range holders {
		switch h.Namespace {
		case NamespaceLocal:
			local = append(local, h)
		case NamespaceOG:
			og = append(og, h)
		}
	}
	if len(local) > 0 && len(og) > 0 {
		if _, reserved := writerclass.Reserved(name); reserved {
			return NameHolder{}, false, false
		}
	}
	switch {
	case len(local) == 1:
		return local[0], len(og) > 0, true
	case len(local) == 0 && len(og) == 1:
		return og[0], false, true
	}
	return NameHolder{}, false, false
}

// ResolveNameIn resolves a name in one namespace only: a qualified name, or the
// boot seed resolving a shipped row's references inside og. Exactly one holder
// there resolves; none, or two (a collision), is nothing.
func ResolveNameIn(namespace string, holders []NameHolder) (NameHolder, bool) {
	var found []NameHolder
	for _, h := range holders {
		if h.Namespace == namespace {
			found = append(found, h)
		}
	}
	if len(found) != 1 {
		return NameHolder{}, false
	}
	return found[0], true
}

// nameHolders reads every catalog row that holds name, in all three ingest
// catalogs.
func nameHolders(ctx context.Context, q querier, name string) ([]NameHolder, error) {
	rows, err := q.Query(ctx, `
		select 'metric', namespace from metric_type where name = $1
		union all select 'property', namespace from property_type where name = $1
		union all select 'event', namespace from event_type where name = $1`, name)
	if err != nil {
		return nil, fmt.Errorf("storage: read the holders of %q: %w", name, err)
	}
	defer rows.Close()
	var out []NameHolder
	for rows.Next() {
		var h NameHolder
		if err := rows.Scan(&h.Lane, &h.Namespace); err != nil {
			return nil, fmt.Errorf("storage: scan a holder of %q: %w", name, err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// namespaceRe is a namespace's shape: one kebab segment, as the column's CHECK
// holds it, so one embedded in SQL can never carry a quote.
var namespaceRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// catalogRows is the row source a by-name lookup of a namespaced catalog reads
// (#1432): the search path, `<table>_in_effect()` (local, then the og rows
// local does not shadow), when namespace is empty; that namespace's rows alone
// otherwise. A caller aliases it: `from `+catalogRows("event_type", ns)+` t`.
// table is always a literal at the call site; a namespace that is not one kebab
// segment reads no rows.
func catalogRows(table, namespace string) string {
	if namespace == "" {
		return table + "_in_effect()"
	}
	if !namespaceRe.MatchString(namespace) {
		return "(select * from " + table + " where false)"
	}
	return "(select * from " + table + " where namespace = '" + namespace + "')"
}

// refRows is the row source a lookup by ref reads, where a ref is a name or a
// uuid (registryRefCol picks the column): every row by uuid, which is unique
// across namespaces, and the search path by name. table is always a literal at
// the call site. A caller aliases it.
func refRows(table, ref string) string {
	if isUUID(ref) || !namespacedCatalogs[table] {
		return table
	}
	return table + "_in_effect()"
}

// namespacedCatalogs are the catalogs a bare name resolves along the search
// path in (#1432). severity_level is not one: its names stay unique
// install-wide.
var namespacedCatalogs = map[string]bool{
	"metric_type": true, "property_type": true, "event_type": true, "command_type": true,
	"component_type": true, "system_type": true, "location_type": true, "secret_type": true,
	"notification_type": true, "event_rule": true, "action_rule": true, "calc_rule": true,
	"vendor": true, "product": true, "driver": true, "standard": true, "role": true,
}

// resolveRefIn turns a registry ref (a name or a uuid) into the row's uuid:
// by uuid any row; by name the search path, or the one namespace given (og when
// the boot seed writes a shipped row's references, #1432). No row is
// pgx.ErrNoRows for the caller to map to its own sentinel.
func resolveRefIn(ctx context.Context, q querier, table, ref, namespace string) (string, error) {
	rows := refRows(table, ref)
	if namespace != "" && !isUUID(ref) {
		rows = catalogRows(table, namespace)
	}
	var id string
	err := q.QueryRow(ctx, `select id::text from `+rows+` t where `+registryRefCol(ref)+` = $1`, ref).Scan(&id)
	return id, err
}

// InEffect keeps the rows a bare name reaches (#1432), the search path applied
// to a list a caller holds: per name, the operator's local row, else the og
// row; a row in any other namespace, or one a local row shadows, is dropped.
// key reads a row's namespace and name. For a caller that indexes a catalog by
// name in Go, so two rows of one name never collide in its map.
func InEffect[T any](rows []T, key func(T) (namespace, name string)) []T {
	local := map[string]bool{}
	for _, r := range rows {
		if ns, name := key(r); ns == NamespaceLocal {
			local[name] = true
		}
	}
	out := make([]T, 0, len(rows))
	for _, r := range rows {
		ns, name := key(r)
		if ns == NamespaceLocal || (ns == NamespaceOG && !local[name]) {
			out = append(out, r)
		}
	}
	return out
}
