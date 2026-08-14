package storage

import "github.com/google/uuid"

// The set-at-a-time type-chain walk (#695, widened by #716).
//
// A classification registry's facts inherit outward: a row that states none
// shows its nearest ancestor's (ADR-0095's first-non-null-wins rule). Resolving
// that per row on a LIST read would be one query per row per level, so this
// resolves the whole listing in one pass over rows the caller already holds.
//
// It resolves the three CONSOLE-FACING facts (stem, icon, abbrev) and not the
// label rule, which is resolved on write paths inside a transaction, where a
// pass over a listing read before the transaction began would be the wrong
// answer.
//
// Each fact carries two answers, and the difference between them is the reason
// this file grew past the icon. [ChainFact.Shown] is what a row displays, which
// is its own value when it states one. [ChainFact.Inherited] is what the row
// would display if it stated none, which is a DIFFERENT string for exactly the
// rows that state their own. A blade's placeholder is the second question ("clear
// this box and you get what?"), and serving only the first would put the value
// an operator had just deleted back in front of them as the thing they were
// about to inherit.
//
// The relationship to [PG.ResolveTypeFacts] is checked rather than asserted:
// TestBothTypeChainWalksAgreeOnEverySeededRow runs both over every seeded row
// and compares all three facts, which is what makes two walks in one package
// safe.

// chainNode is the shape both registries reduce to for the walk: the parent to
// climb to, the name this node answers to, and the facts it states itself.
//
// A nil fact is INHERITING and an empty string is a value, the same distinction
// the query walk makes on the nullable column. That difference is invisible on
// the wire (both render as an absent field), which is exactly why the resolution
// belongs on this side of it.
type chainNode struct {
	parentID *uuid.UUID
	name     string
	stem     *string
	icon     *string
	abbrev   *string
}

// ChainFact is one inherited fact, resolved over a registry chain.
type ChainFact struct {
	// Shown is the value the row displays: its own when it states one, else the
	// nearest ancestor's. Empty when nothing in the chain states one; the
	// fallback differs per registry and belongs to whoever draws it.
	Shown string
	// Inherited is the value the row would display if it stated none: the
	// nearest ANCESTOR's, empty when no ancestor states one. It equals Shown on
	// a row that states nothing, and differs on every row that does.
	Inherited string
	// InheritedFrom is the NAME of the ancestor Inherited came from, empty when
	// there is none. It is read from the data rather than assumed to be the
	// parent, because a fact can come from any distance up the chain, and it is
	// per fact rather than per row, because one row can take its stem from a
	// grandparent and its abbrev from its parent.
	InheritedFrom string
}

// TypeChainFacts is one registry row's three inherited facts.
type TypeChainFacts struct {
	Stem   ChainFact
	Icon   ChainFact
	Abbrev ChainFact
}

// ComponentTypeChainFacts returns the resolved facts of every row, keyed by id,
// over a component_type listing the caller already holds.
func ComponentTypeChainFacts(types []ComponentType) map[uuid.UUID]TypeChainFacts {
	nodes := make(map[uuid.UUID]chainNode, len(types))
	for i := range types {
		nodes[types[i].ID] = chainNode{
			parentID: types[i].ParentID, name: types[i].Name,
			stem: types[i].Stem, icon: types[i].Icon, abbrev: types[i].Abbrev,
		}
	}
	return resolveTypeChains(nodes, maxComponentTypeDepth)
}

// SystemTypeChainFacts is [ComponentTypeChainFacts] for the system_type
// registry, which nests by the same rule with its own depth bound.
func SystemTypeChainFacts(types []SystemType) map[uuid.UUID]TypeChainFacts {
	nodes := make(map[uuid.UUID]chainNode, len(types))
	for i := range types {
		nodes[types[i].ID] = chainNode{
			parentID: types[i].ParentID, name: types[i].Name,
			stem: types[i].Stem, icon: types[i].Icon, abbrev: types[i].Abbrev,
		}
	}
	return resolveTypeChains(nodes, maxSystemTypeDepth)
}

// resolveTypeChains resolves every node's three facts. Pure: rows in, answers
// out, one pass per fact per row over a map already in hand.
func resolveTypeChains(nodes map[uuid.UUID]chainNode, maxDepth int) map[uuid.UUID]TypeChainFacts {
	out := make(map[uuid.UUID]TypeChainFacts, len(nodes))
	for id := range nodes {
		out[id] = TypeChainFacts{
			Stem:   resolveChainFact(nodes, id, maxDepth, func(n chainNode) *string { return n.stem }),
			Icon:   resolveChainFact(nodes, id, maxDepth, func(n chainNode) *string { return n.icon }),
			Abbrev: resolveChainFact(nodes, id, maxDepth, func(n chainNode) *string { return n.abbrev }),
		}
	}
	return out
}

// resolveChainFact climbs id's parents for one fact, answering both what the row
// shows and what it would inherit. It does NOT stop at the row's own value: a
// row that states its own still has an answer to the placeholder question, which
// is the first ancestor's, so the climb continues past it.
//
// The climb is bounded by depth rather than by a visited set, the same backstop
// the query walks use, so a cycle terminates with an unresolved fact instead of
// hanging. A parent id naming a row the map does not carry stops the climb the
// same way a root does; through the API that cannot happen (the listing is the
// whole registry), and pinning it is what the day somebody filters the list
// inherits.
func resolveChainFact(nodes map[uuid.UUID]chainNode, id uuid.UUID, maxDepth int, pick func(chainNode) *string) ChainFact {
	var f ChainFact
	own := false
	cur := id
	for i := range maxDepth {
		node, found := nodes[cur]
		if !found {
			break
		}
		if v := pick(node); v != nil {
			if i == 0 {
				f.Shown, own = *v, true
			} else {
				f.Inherited, f.InheritedFrom = *v, node.name
				if !own {
					f.Shown = *v
				}
				break
			}
		}
		if node.parentID == nil {
			break
		}
		cur = *node.parentID
	}
	return f
}
