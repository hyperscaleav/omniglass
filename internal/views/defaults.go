package views

import (
	"context"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Defaults returns the default view set shipped with the binary. Each entry is
// declared Go, PR-governed like seed data; the catalog grows view by view
// (event-feed, sample-history, and estate-counts land with #541).
func Defaults() []Definition {
	return []Definition{componentReachability()}
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
		FieldMapping: map[string]string{"value": "state", "label": "component", "time": "since"},
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
