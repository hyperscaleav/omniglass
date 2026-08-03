package views

import (
	"context"
	"errors"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Defaults returns the default view set shipped with the binary. Each entry is
// declared Go, PR-governed like seed data; the catalog grows view by view.
func Defaults() []Definition {
	return []Definition{
		componentReachability(),
		eventFeed(),
		sampleHistory(),
		estateCounts(),
	}
}

// defaultHistoryWindow is how far sample-history looks back when the caller
// names no window; defaultFeedPage is the feed's page size when none is asked
// for.
const (
	defaultHistoryWindow = 24 * time.Hour
	defaultFeedPage      = 50
)

// requireVisibleComponent verifies a parameterized target is in the caller's
// read scope. An absent or out-of-scope component is ErrTargetNotFound, which
// the route answers as one non-disclosing 404 for both. Any OTHER failure is
// returned as itself, so a database outage surfaces as a 500 rather than
// telling an operator their component no longer exists.
func requireVisibleComponent(ctx context.Context, gw storage.Gateway, name string, read scope.Set) error {
	if _, err := gw.GetComponent(ctx, name, read); err != nil {
		if errors.Is(err, storage.ErrComponentNotFound) {
			return ErrTargetNotFound
		}
		return err
	}
	return nil
}

// eventFeed is the cursor-paged occurrence feed, newest first, optionally
// narrowed to one component. The page token carries the last row's keyset, so
// a walk cannot duplicate or skip a row when the feed grows mid-walk.
func eventFeed() Definition {
	return Definition{
		Name:          "event-feed",
		Summary:       "The estate's occurrences, newest first, cursor-paged",
		Permission:    []string{"event", "read"},
		ScopeResource: "component",
		Params: []Param{
			{Name: "owner", Type: "string", Doc: "Narrow the feed to one component by name"},
			{Name: "limit", Type: "int", Doc: "Rows per page (default 50, capped at 500)"},
		},
		Columns: []Column{
			{Name: "ts", Type: "time", Role: "time"},
			{Name: "owner", Type: "string", Role: "label"},
			{Name: "event_type", Type: "string"},
			{Name: "instance", Type: "string"},
			{Name: "origin", Type: "string"},
			{Name: "message", Type: "string", Role: "value"},
			{Name: "source", Type: "string"},
		},
		FieldMapping: map[string]string{"value": "message", "label": "owner", "time": "ts"},
		Run: func(ctx context.Context, gw storage.Gateway, read scope.Set, params map[string]any, pageToken string) ([][]any, string, error) {
			cur, err := decodeCursor(pageToken)
			if err != nil {
				return nil, "", err
			}
			q := storage.EventFeedQuery{Limit: defaultFeedPage, BeforeTS: cur.TS, BeforeID: cur.ID}
			if n, ok := params["limit"].(int); ok && n > 0 {
				// Clamp HERE, to the same ceiling storage enforces. Passing an
				// un-clamped limit through would make the full-page test below
				// compare a capped row count against a bigger number, read the
				// page as the last one, and end the walk with rows unread.
				q.Limit = min(n, storage.MaxViewPage)
			}
			if owner, ok := params["owner"].(string); ok && owner != "" {
				// The owner filter is a parameterized target: verify it is in
				// the caller's scope first, so an out-of-scope name is a
				// non-disclosing 404 rather than a silently empty feed.
				if err := requireVisibleComponent(ctx, gw, owner, read); err != nil {
					return nil, "", err
				}
				q.Owner = owner
			}
			recs, err := gw.ListEventFeed(ctx, read, q)
			if err != nil {
				return nil, "", err
			}
			rows := make([][]any, 0, len(recs))
			for _, r := range recs {
				rows = append(rows, []any{r.TS, r.Owner, r.Key, r.Instance, r.Origin, r.Message, r.Source})
			}
			// A full page implies there may be more; a short page ends the
			// walk, so a client stops without an extra empty request.
			var next string
			if len(recs) == q.Limit {
				last := recs[len(recs)-1]
				next = encodeCursor(cursor{TS: last.TS, ID: last.ID})
			}
			return rows, next, nil
		},
	}
}

// sampleHistory is one component's readings for one property key over a
// window, oldest first. It reads both sample lanes: a numeric reading fills
// value, a categorical one fills state, so a chart and a status strip read the
// same view without a column whose type depends on the row.
//
// Strictly historical by decision: the current value already sits on the
// component blade, and folding it in would make the series' last point mean
// something different from every other point.
func sampleHistory() Definition {
	return Definition{
		Name:          "sample-history",
		Summary:       "One component's readings for a property key over a window, oldest first",
		Permission:    []string{"component", "read"},
		ScopeResource: "component",
		Params: []Param{
			{Name: "owner", Type: "string", Required: true, Doc: "The component name"},
			{Name: "key", Type: "string", Required: true, Doc: "The property type key (icmp.rtt_avg, interface.reachable, ...)"},
			{Name: "instance", Type: "string", Doc: "Narrow to one series instance (an interface name)"},
			{Name: "window", Type: "duration", Doc: "How far back to read (default 24h)"},
		},
		Columns: []Column{
			{Name: "ts", Type: "time", Role: "time"},
			{Name: "instance", Type: "string", Role: "series"},
			{Name: "value", Type: "number", Role: "value"},
			{Name: "state", Type: "string"},
		},
		FieldMapping: map[string]string{"value": "value", "series": "instance", "time": "ts"},
		Run: func(ctx context.Context, gw storage.Gateway, read scope.Set, params map[string]any, _ string) ([][]any, string, error) {
			owner, _ := params["owner"].(string)
			key, _ := params["key"].(string)
			// The owner is a parameterized target: out of scope is a
			// non-disclosing 404, exactly as on the component's own reads.
			if err := requireVisibleComponent(ctx, gw, owner, read); err != nil {
				return nil, "", err
			}
			window := defaultHistoryWindow
			if d, ok := params["window"].(time.Duration); ok && d > 0 {
				window = d
			}
			q := storage.SampleHistoryQuery{Owner: owner, Key: key, Since: time.Now().UTC().Add(-window)}
			if inst, ok := params["instance"].(string); ok {
				q.Instance = inst
			}
			recs, err := gw.ListSampleHistory(ctx, read, q)
			if err != nil {
				return nil, "", err
			}
			rows := make([][]any, 0, len(recs))
			for _, r := range recs {
				var value any
				if r.Value != nil {
					value = *r.Value
				}
				rows = append(rows, []any{r.TS, r.Instance, value, r.State})
			}
			return rows, "", nil
		},
	}
}

// estateCounts is the Home stat tiles: how much estate the caller can see and
// how it currently reads. Its interface tiles read a resource this view does not
// declare a permission for, which a permission SET per view fixes (#548); scope
// still bounds every tile, so the gap is a capability under-declaration, not a
// visibility leak.
func estateCounts() Definition {
	return Definition{
		Name:          "estate-counts",
		Summary:       "The stat tiles: components and interfaces in scope, and their current verdicts",
		Permission:    []string{"component", "read"},
		ScopeResource: "component",
		Columns: []Column{
			{Name: "tile", Type: "string", Role: "label"},
			{Name: "count", Type: "number", Role: "value"},
		},
		FieldMapping: map[string]string{"value": "count", "label": "tile"},
		Run: func(ctx context.Context, gw storage.Gateway, read scope.Set, _ map[string]any, _ string) ([][]any, string, error) {
			c, err := gw.EstateCounts(ctx, read)
			if err != nil {
				return nil, "", err
			}
			// A fixed row order, declared here rather than sorted: these are
			// tiles in a row, not a list, and the order is the layout.
			return [][]any{
				{"components", c.Components},
				{"interfaces", c.Interfaces},
				{"reachable", c.InterfacesUp},
				{"unreachable", c.InterfacesDown},
			}, "", nil
		},
	}
}

// componentReachability is the fleet grid over the interface.reachable state:
// one row per in-scope component interface with its latest verdict. A
// never-probed interface reports the explicit "unknown" state and a null
// since, so the grid renders the whole fleet, not only the probed part.
func componentReachability() Definition {
	return Definition{
		Name:          "component-reachability",
		Summary:       "Every in-scope component interface with its latest reachability verdict",
		Permission:    []string{"component", "read"},
		ScopeResource: "component",
		Columns: []Column{
			{Name: "component", Type: "string", Role: "label"},
			{Name: "interface", Type: "string"},
			{Name: "interface_type", Type: "string"},
			{Name: "state", Type: "string", Role: "value"},
			{Name: "since", Type: "time", Role: "time"},
		},
		// sublabel distinguishes rows sharing a label: a component with two
		// interfaces renders as two chips, and without the interface name they
		// would be indistinguishable, so a grid could not say which one is down.
		FieldMapping: map[string]string{"value": "state", "label": "component", "sublabel": "interface", "time": "since"},
		Run: func(ctx context.Context, gw storage.Gateway, read scope.Set, _ map[string]any, _ string) ([][]any, string, error) {
			recs, err := gw.ListInterfaceReachability(ctx, read)
			if err != nil {
				return nil, "", err
			}
			rows := make([][]any, 0, len(recs))
			for _, r := range recs {
				state := r.Value
				if state == "" {
					state = "unknown"
				}
				var since any
				if r.TS != nil {
					since = *r.TS
				}
				rows = append(rows, []any{r.Component, r.Interface, r.Type, state, since})
			}
			return rows, "", nil
		},
	}
}
