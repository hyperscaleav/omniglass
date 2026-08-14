package storage_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The set-at-a-time type-chain walk (#695, widened by #716's console half).
// Until #695, the console climbed the type chain in TypeScript for icons while
// the gateway climbed it in Go, and the two walks could disagree about which
// glyph a type shows. This resolves it over a registry listing the caller
// already holds, so the answer is served rather than recomputed in the browser.
//
// It is pure by construction: rows in, resolved facts out, no querier and no
// context. That is the whole reason it can be tested at this altitude, and the
// reason the equivalence test against the query walk (typechain_agree_test.go)
// is the only place a database is needed.
//
// It answers TWO questions per fact, and the difference between them is the
// whole of #716's console half. What a row SHOWS is its own value when it
// states one; what a row INHERITS is the nearest ANCESTOR's, which is a
// different answer for exactly the rows that state their own, and is what a
// blade's placeholder has to say ("clear this box and you get what?"). Serving
// only the first would make an emptied box show the value the operator just
// deleted.

// chainRow is one node of a test chain: what it is called, who its parent is,
// and which of the three inherited facts it states. A fact given as "" is one
// the row does not state (nil in storage), which is the distinction the whole
// walk turns on.
type chainRow struct {
	id, parent         uuid.UUID
	name               string
	stem, icon, abbrev string
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ctNode(r chainRow) storage.ComponentType {
	ct := storage.ComponentType{ID: r.id, Name: r.name, Stem: strOrNil(r.stem), Icon: strOrNil(r.icon), Abbrev: strOrNil(r.abbrev)}
	if r.parent != uuid.Nil {
		p := r.parent
		ct.ParentID = &p
	}
	return ct
}

func stNode(r chainRow) storage.SystemType {
	st := storage.SystemType{ID: r.id, Name: r.name, Stem: strOrNil(r.stem), Icon: strOrNil(r.icon), Abbrev: strOrNil(r.abbrev)}
	if r.parent != uuid.Nil {
		p := r.parent
		st.ParentID = &p
	}
	return st
}

func TestComponentTypeChainFactsClimbToTheNearestAncestorThatSetsOne(t *testing.T) {
	root, mid, leaf := uuid.New(), uuid.New(), uuid.New()
	got := storage.ComponentTypeChainFacts([]storage.ComponentType{
		ctNode(chainRow{id: root, name: "root", icon: "layers"}),
		ctNode(chainRow{id: mid, parent: root, name: "mid", icon: "door-open"}),
		ctNode(chainRow{id: leaf, parent: mid, name: "leaf"}),
	})
	if got[leaf].Icon.Shown != "door-open" {
		t.Errorf("leaf icon = %q, want the NEAREST ancestor's %q rather than the root's", got[leaf].Icon.Shown, "door-open")
	}
	if got[mid].Icon.Shown != "door-open" {
		t.Errorf("mid icon = %q, want its own %q", got[mid].Icon.Shown, "door-open")
	}
	if got[root].Icon.Shown != "layers" {
		t.Errorf("root icon = %q, want its own %q", got[root].Icon.Shown, "layers")
	}
}

// A chain that sets no icon anywhere resolves to nothing, not to a default. The
// fallback glyph is the console's to choose (it differs per registry), and a
// server that invented one here would make an absence indistinguishable from a
// type that really is drawn as a box.
func TestComponentTypeChainFactsLeaveAnUnsetChainEmpty(t *testing.T) {
	root, leaf := uuid.New(), uuid.New()
	got := storage.ComponentTypeChainFacts([]storage.ComponentType{
		ctNode(chainRow{id: root, name: "root"}),
		ctNode(chainRow{id: leaf, parent: root, name: "leaf"}),
	})
	if got[leaf].Icon.Shown != "" {
		t.Errorf("leaf icon = %q, want empty", got[leaf].Icon.Shown)
	}
	if got[leaf].Icon.Inherited != "" || got[leaf].Icon.InheritedFrom != "" {
		t.Errorf("leaf inherited icon = %q from %q, want neither: nothing above states one",
			got[leaf].Icon.Inherited, got[leaf].Icon.InheritedFrom)
	}
}

// A parent id pointing at a row the listing does not carry stops the walk
// rather than panicking. It cannot happen through the API (the listing is the
// whole registry, unfiltered), which is exactly why it is pinned: the day
// somebody filters the list, this is the behaviour they inherit.
func TestComponentTypeChainFactsStopAtAnAbsentParent(t *testing.T) {
	orphan := uuid.New()
	got := storage.ComponentTypeChainFacts([]storage.ComponentType{ctNode(chainRow{id: orphan, parent: uuid.New(), name: "orphan"})})
	if got[orphan].Icon.Shown != "" {
		t.Errorf("orphan icon = %q, want empty", got[orphan].Icon.Shown)
	}
	if got[orphan].Icon.InheritedFrom != "" {
		t.Errorf("orphan inherited from %q, want no source: the ancestor is not in the listing", got[orphan].Icon.InheritedFrom)
	}
}

// A cycle is data a client sent us, not data the registry can hold, and the
// walk is depth-bounded rather than visited-set-bounded for the same reason the
// query walk is: the bound is the thing under test, so a cycle must terminate.
func TestComponentTypeChainFactsTerminateOnACycle(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	done := make(chan map[uuid.UUID]storage.TypeChainFacts, 1)
	go func() {
		done <- storage.ComponentTypeChainFacts([]storage.ComponentType{
			ctNode(chainRow{id: a, parent: b, name: "a"}),
			ctNode(chainRow{id: b, parent: a, name: "b"}),
		})
	}()
	select {
	case got := <-done:
		if got[a].Icon.Shown != "" || got[b].Icon.Shown != "" {
			t.Errorf("cycle resolved to %q / %q, want empty", got[a].Icon.Shown, got[b].Icon.Shown)
		}
	case <-t.Context().Done():
		t.Fatal("the walk did not terminate on a cycle")
	}
}

func TestSystemTypeChainFactsClimbTheSameWay(t *testing.T) {
	root, mid, leaf := uuid.New(), uuid.New(), uuid.New()
	got := storage.SystemTypeChainFacts([]storage.SystemType{
		stNode(chainRow{id: root, name: "root", icon: "layers"}),
		stNode(chainRow{id: mid, parent: root, name: "mid", icon: "door-open"}),
		stNode(chainRow{id: leaf, parent: mid, name: "leaf"}),
	})
	if got[leaf].Icon.Shown != "door-open" {
		t.Errorf("leaf icon = %q, want %q", got[leaf].Icon.Shown, "door-open")
	}
}

// The placeholder question, and the one a "what does this row show" walk cannot
// answer. `mid` states a stem of its own, so what it SHOWS is that stem; what it
// would show with the box emptied is the root's, which is a different string.
// A console that took Shown for a placeholder would print the value the operator
// had just deleted back at them as the thing they were about to inherit.
func TestChainFactsSeparateWhatARowShowsFromWhatItWouldInherit(t *testing.T) {
	root, mid := uuid.New(), uuid.New()
	got := storage.ComponentTypeChainFacts([]storage.ComponentType{
		ctNode(chainRow{id: root, name: "mic", stem: "mic"}),
		ctNode(chainRow{id: mid, parent: root, name: "ceiling-mic", stem: "ceilmic"}),
	})
	if got[mid].Stem.Shown != "ceilmic" {
		t.Errorf("mid stem shown = %q, want its own %q", got[mid].Stem.Shown, "ceilmic")
	}
	if got[mid].Stem.Inherited != "mic" {
		t.Errorf("mid inherited stem = %q, want the ancestor's %q, which is what an emptied box would take", got[mid].Stem.Inherited, "mic")
	}
	if got[mid].Stem.InheritedFrom != "mic" {
		t.Errorf("mid inherited stem from %q, want the ancestor %q", got[mid].Stem.InheritedFrom, "mic")
	}
	// A root has nothing above it, so there is no placeholder to offer.
	if got[root].Stem.Inherited != "" || got[root].Stem.InheritedFrom != "" {
		t.Errorf("root inherited stem = %q from %q, want neither", got[root].Stem.Inherited, got[root].Stem.InheritedFrom)
	}
}

// The source is read from the DATA rather than assumed to be the parent, which
// is the acceptance the hint's wording rests on: `leaf` inherits its stem from
// its GRANDparent, because the row between them states none, and a hint that
// said "its parent" would name the wrong row to go and change.
//
// The three facts are resolved independently, so one row can inherit its stem
// from the grandparent and its abbrev from the parent at the same time. That is
// the case here, and it is the reason each fact carries its own source rather
// than the row carrying one.
func TestChainFactsNameTheAncestorEachFactActuallyCameFrom(t *testing.T) {
	root, mid, leaf := uuid.New(), uuid.New(), uuid.New()
	got := storage.ComponentTypeChainFacts([]storage.ComponentType{
		ctNode(chainRow{id: root, name: "mic", stem: "mic", abbrev: "mic", icon: "mic"}),
		ctNode(chainRow{id: mid, parent: root, name: "ceiling-mic", abbrev: "cmic"}),
		ctNode(chainRow{id: leaf, parent: mid, name: "ceiling-array"}),
	})
	if got[leaf].Stem.Inherited != "mic" || got[leaf].Stem.InheritedFrom != "mic" {
		t.Errorf("leaf stem = %q from %q, want %q from the GRANDparent %q",
			got[leaf].Stem.Inherited, got[leaf].Stem.InheritedFrom, "mic", "mic")
	}
	if got[leaf].Abbrev.Inherited != "cmic" || got[leaf].Abbrev.InheritedFrom != "ceiling-mic" {
		t.Errorf("leaf abbrev = %q from %q, want %q from the parent %q",
			got[leaf].Abbrev.Inherited, got[leaf].Abbrev.InheritedFrom, "cmic", "ceiling-mic")
	}
	// mid states its own abbrev, so its inherited one is still the root's.
	if got[mid].Abbrev.Shown != "cmic" || got[mid].Abbrev.Inherited != "mic" {
		t.Errorf("mid abbrev shown/inherited = %q/%q, want %q/%q", got[mid].Abbrev.Shown, got[mid].Abbrev.Inherited, "cmic", "mic")
	}
}

// An empty string a row really states is a VALUE and not an absence, the same
// distinction the nullable column makes and the one that is invisible on the
// wire. A row stating "" stops the walk: it shows nothing on purpose, and its
// descendants inherit that nothing rather than climbing past it.
func TestChainFactsTreatAStatedEmptyStringAsAValue(t *testing.T) {
	root, mid := uuid.New(), uuid.New()
	stated := ""
	rootRow := ctNode(chainRow{id: root, name: "mic", icon: "mic"})
	midRow := ctNode(chainRow{id: mid, parent: root, name: "ceiling-mic"})
	midRow.Icon = &stated
	got := storage.ComponentTypeChainFacts([]storage.ComponentType{rootRow, midRow})
	if got[mid].Icon.Shown != "" {
		t.Errorf("mid icon = %q, want the empty string it states rather than the ancestor's", got[mid].Icon.Shown)
	}
	if got[mid].Icon.Inherited != "mic" {
		t.Errorf("mid inherited icon = %q, want the ancestor's %q: clearing the state would reach it", got[mid].Icon.Inherited, "mic")
	}
}
