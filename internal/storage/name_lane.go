package storage

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// NameHolder is one catalog row holding a name: which catalog ("metric",
// "property" or "event"), and whether the row is the platform's own.
type NameHolder struct {
	Lane     string
	Official bool
}

// ResolveNameLane is the one rule for which catalog a bare name resolves on
// (#1307), shared by the ingest registry and every reader of a name (an event
// rule's sample key, a calc rule's key, a driver's emit). The three ingest
// catalogs share one namespace, and a name an official row holds in one catalog
// and an operator's row holds in another (a release shipped a name an operator
// had already coined) resolves to the operator's row: private shadows official,
// the platform's row staying where it is. So:
//   - a name one catalog holds resolves there;
//   - a name held more than once resolves to the operator's row when exactly one
//     holder is an operator's and the rest are official (shadowed is true);
//   - any other name held more than once (two official rows, two operator rows,
//     an operator row among several) resolves to nothing: ok is false;
//   - a reserved key, the node's or the platform's own, is never shadowed: held
//     twice, it resolves to nothing.
//
// ok is false for a name nothing holds too.
func ResolveNameLane(name string, holders []NameHolder) (lane string, shadowed, ok bool) {
	switch len(holders) {
	case 0:
		return "", false, false
	case 1:
		return holders[0].Lane, false, true
	}
	if _, reserved := writerclass.Reserved(name); reserved {
		return "", false, false
	}
	var operators []NameHolder
	for _, h := range holders {
		if !h.Official {
			operators = append(operators, h)
		}
	}
	if len(operators) != 1 {
		return "", false, false
	}
	return operators[0].Lane, true, true
}

// nameHolders reads every catalog row that holds name, in all three ingest
// catalogs.
func nameHolders(ctx context.Context, q querier, name string) ([]NameHolder, error) {
	rows, err := q.Query(ctx, `
		select 'metric', official from metric_type where name = $1
		union all select 'property', official from property_type where name = $1
		union all select 'event', official from event_type where name = $1`, name)
	if err != nil {
		return nil, fmt.Errorf("storage: read the holders of %q: %w", name, err)
	}
	defer rows.Close()
	var out []NameHolder
	for rows.Next() {
		var h NameHolder
		if err := rows.Scan(&h.Lane, &h.Official); err != nil {
			return nil, fmt.Errorf("storage: scan a holder of %q: %w", name, err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
