package storage

import "github.com/google/uuid"

// The set-at-a-time type-chain walk (#695).
//
// A classification registry's facts inherit outward: a row that states none
// shows its nearest ancestor's (ADR-0095's first-non-null-wins rule). Resolving
// that per row on a LIST read would be one query per row per level, so this
// resolves the whole listing in one pass over rows the caller already holds.
//
// It resolves the ICON and nothing else, deliberately. The icon is the one
// inherited fact a reader outside the gateway needs resolved: the console draws
// a glyph for every registry row and, before this, climbed the chain itself in
// TypeScript against this package's climb in Go. The stem, the abbrev and the
// label rule are resolved on write paths inside a transaction, where a pass over
// a listing read before the transaction began would be the wrong answer.
//
// The relationship to [PG.ResolveTypeFacts] is checked rather than asserted:
// TestBothTypeChainWalksAgreeOnEverySeededRow runs both over every seeded row
// and compares, which is what makes two walks in one package safe.

// iconChainNode is the shape both registries reduce to for the walk: the parent
// to climb to, and the icon this node itself states.
//
// A nil icon is INHERITING and an empty string is a value, the same distinction
// the query walk makes on the nullable column. That difference is invisible on
// the wire (both render as an absent field), which is exactly why the resolution
// belongs on this side of it.
type iconChainNode struct {
	parentID *uuid.UUID
	icon     *string
}

// ResolvedComponentTypeIcons returns the icon each row SHOWS, keyed by id, over
// a component_type listing the caller already holds. A chain that states no icon
// anywhere resolves to the empty string rather than to a default glyph: the
// fallback differs per registry and belongs to whoever draws it.
func ResolvedComponentTypeIcons(types []ComponentType) map[uuid.UUID]string {
	nodes := make(map[uuid.UUID]iconChainNode, len(types))
	for i := range types {
		nodes[types[i].ID] = iconChainNode{parentID: types[i].ParentID, icon: types[i].Icon}
	}
	return resolveIconChains(nodes, maxComponentTypeDepth)
}

// ResolvedSystemTypeIcons is [ResolvedComponentTypeIcons] for the system_type
// registry, which nests by the same rule with its own depth bound.
func ResolvedSystemTypeIcons(types []SystemType) map[uuid.UUID]string {
	nodes := make(map[uuid.UUID]iconChainNode, len(types))
	for i := range types {
		nodes[types[i].ID] = iconChainNode{parentID: types[i].ParentID, icon: types[i].Icon}
	}
	return resolveIconChains(nodes, maxSystemTypeDepth)
}

// resolveIconChains climbs every node's parents to the first one that states an
// icon. Pure: rows in, answers out.
//
// The climb is bounded by depth rather than by a visited set, the same backstop
// the query walks use, so a cycle terminates with an unresolved icon instead of
// hanging. A parent id naming a row the map does not carry stops the climb the
// same way a root does; through the API that cannot happen (the listing is the
// whole registry), and pinning it is what the day somebody filters the list
// inherits.
func resolveIconChains(nodes map[uuid.UUID]iconChainNode, maxDepth int) map[uuid.UUID]string {
	out := make(map[uuid.UUID]string, len(nodes))
	for id := range nodes {
		cur := id
		for range maxDepth {
			node, found := nodes[cur]
			if !found {
				break
			}
			if node.icon != nil {
				out[id] = *node.icon
				break
			}
			if node.parentID == nil {
				break
			}
			cur = *node.parentID
		}
	}
	return out
}
