package storage_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The set-at-a-time icon walk (#695). Until this, the console climbed the type
// chain in TypeScript for icons while the gateway climbed it in Go, and the two
// walks could disagree about which glyph a type shows. This resolves it over a
// registry listing the caller already holds, so the answer is served rather than
// recomputed in the browser.
//
// It is pure by construction: rows in, resolved icons out, no querier and no
// context. That is the whole reason it can be tested at this altitude, and the
// reason the equivalence test against the query walk (typechain_agree_test.go)
// is the only place a database is needed.

func ctNode(id, parent uuid.UUID, icon string) storage.ComponentType {
	ct := storage.ComponentType{ID: id}
	if parent != uuid.Nil {
		p := parent
		ct.ParentID = &p
	}
	if icon != "" {
		i := icon
		ct.Icon = &i
	}
	return ct
}

func TestResolvedComponentTypeIconsClimbToTheNearestAncestorThatSetsOne(t *testing.T) {
	root, mid, leaf := uuid.New(), uuid.New(), uuid.New()
	got := storage.ResolvedComponentTypeIcons([]storage.ComponentType{
		ctNode(root, uuid.Nil, "layers"),
		ctNode(mid, root, "door-open"),
		ctNode(leaf, mid, ""),
	})
	if got[leaf] != "door-open" {
		t.Errorf("leaf icon = %q, want the NEAREST ancestor's %q rather than the root's", got[leaf], "door-open")
	}
	if got[mid] != "door-open" {
		t.Errorf("mid icon = %q, want its own %q", got[mid], "door-open")
	}
	if got[root] != "layers" {
		t.Errorf("root icon = %q, want its own %q", got[root], "layers")
	}
}

// A chain that sets no icon anywhere resolves to nothing, not to a default. The
// fallback glyph is the console's to choose (it differs per registry), and a
// server that invented one here would make an absence indistinguishable from a
// type that really is drawn as a box.
func TestResolvedComponentTypeIconsLeaveAnUnsetChainEmpty(t *testing.T) {
	root, leaf := uuid.New(), uuid.New()
	got := storage.ResolvedComponentTypeIcons([]storage.ComponentType{
		ctNode(root, uuid.Nil, ""),
		ctNode(leaf, root, ""),
	})
	if got[leaf] != "" {
		t.Errorf("leaf icon = %q, want empty", got[leaf])
	}
}

// A parent id pointing at a row the listing does not carry stops the walk
// rather than panicking. It cannot happen through the API (the listing is the
// whole registry, unfiltered), which is exactly why it is pinned: the day
// somebody filters the list, this is the behaviour they inherit.
func TestResolvedComponentTypeIconsStopAtAnAbsentParent(t *testing.T) {
	orphan := uuid.New()
	got := storage.ResolvedComponentTypeIcons([]storage.ComponentType{ctNode(orphan, uuid.New(), "")})
	if got[orphan] != "" {
		t.Errorf("orphan icon = %q, want empty", got[orphan])
	}
}

// A cycle is data a client sent us, not data the registry can hold, and the
// walk is depth-bounded rather than visited-set-bounded for the same reason the
// query walk is: the bound is the thing under test, so a cycle must terminate.
func TestResolvedComponentTypeIconsTerminateOnACycle(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	done := make(chan map[uuid.UUID]string, 1)
	go func() {
		done <- storage.ResolvedComponentTypeIcons([]storage.ComponentType{
			ctNode(a, b, ""),
			ctNode(b, a, ""),
		})
	}()
	select {
	case got := <-done:
		if got[a] != "" || got[b] != "" {
			t.Errorf("cycle resolved to %q / %q, want empty", got[a], got[b])
		}
	case <-t.Context().Done():
		t.Fatal("the walk did not terminate on a cycle")
	}
}

func TestResolvedSystemTypeIconsClimbTheSameWay(t *testing.T) {
	root, mid, leaf := uuid.New(), uuid.New(), uuid.New()
	rootIcon, midIcon := "layers", "door-open"
	midP, leafP := root, mid
	got := storage.ResolvedSystemTypeIcons([]storage.SystemType{
		{ID: root, Icon: &rootIcon},
		{ID: mid, ParentID: &midP, Icon: &midIcon},
		{ID: leaf, ParentID: &leafP},
	})
	if got[leaf] != "door-open" {
		t.Errorf("leaf icon = %q, want %q", got[leaf], "door-open")
	}
}
