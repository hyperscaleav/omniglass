package api

import (
	"context"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// treeActions are the per-row affordances the console gates on a tree entity:
// create (a child under this row), update, and delete. read is omitted, since
// every row the list returns is already in the caller's read scope by
// construction.
var treeActions = []string{"create", "update", "delete"}

// rowActions computes, for a page of tree-entity rows, which of create/update/
// delete the caller may perform on each, using the SAME per-action scope the
// gateway enforces. One InScopeIDs query per action answers the whole page, so
// the console renders exactly the affordances the server would allow, a hint that
// cannot disagree with enforcement because it shares the resolution. The server
// stays the only authority; this only decides which buttons to show.
func (a *authenticator) rowActions(ctx context.Context, gw storage.Gateway, resource string, ids []string) (map[string][]string, error) {
	perID := make(map[string][]string, len(ids))
	for _, action := range treeActions {
		inScope, err := gw.InScopeIDs(ctx, resource, ids, a.scopeFor(ctx, resource, action))
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if inScope[id] {
				perID[id] = append(perID[id], action)
			}
		}
	}
	return perID, nil
}
