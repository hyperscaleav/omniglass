package label_test

import (
	"sync"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/label"
)

// The engine's LIFECYCLE (#684). Until the acronym list became an operator
// setting, one immutable package-level engine was the whole story. A dictionary
// an operator edits at runtime turns that constant into something with a life:
// the engine in use has to follow the current list, and it has to do so without
// a restart and without a per-render rebuild.
//
// [label.EngineCache] is that lifecycle, and its key IS the dictionary, so there
// is no generation counter for a caller to bump and forget.

// engineFor is the two-line rule under test: ask for the engine for a
// dictionary, render one rule through it.
func renderWith(t *testing.T, e *label.Engine, src string, d label.Data) string {
	t.Helper()
	r, err := e.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	out, err := r.Render(d)
	if err != nil {
		t.Fatalf("render %q: %v", src, err)
	}
	return out
}

// An unchanged dictionary is the common case by a wide margin: every entity
// write renders a label, and the list changes about never. Rebuilding the
// FuncMap on each of them would be work with no result to show for it.
func TestEngineCacheReusesTheEngineForAnUnchangedDictionary(t *testing.T) {
	var c label.EngineCache
	first := c.For([]string{"AV", "DSP"})
	second := c.For([]string{"AV", "DSP"})
	if first != second {
		t.Fatalf("an unchanged dictionary must reuse the engine, got two")
	}
}

// The headline behavior: an operator adds an acronym and the next render uses
// it, with no restart. The second half of the assertion is why the cache hands
// back a REPLACEMENT rather than mutating an engine in place. Parsing binds the
// FuncMap, so a rule compiled against the old dictionary keeps titling the old
// way forever; the swap is only visible to a rule parsed after it, which is what
// makes "every render re-parses" a correctness requirement on the storage path
// rather than a wasteful habit.
func TestEngineCacheRebuildsWhenTheDictionaryChanges(t *testing.T) {
	var c label.EngineCache
	before := c.For([]string{"AV"})
	compiledBefore, err := before.Parse("{{.A | title}}")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	after := c.For([]string{"AV", "QM55"})
	if before == after {
		t.Fatalf("a changed dictionary must yield a new engine, got the cached one")
	}
	if got := renderWith(t, after, "{{.A | title}}", label.Data{"A": "qm55 av panel"}); got != "QM55 AV Panel" {
		t.Fatalf("after the edit, title = %q, want %q", got, "QM55 AV Panel")
	}

	stale, err := compiledBefore.Render(label.Data{"A": "qm55 av panel"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if stale != "Qm55 AV Panel" {
		t.Fatalf("a rule compiled before the edit = %q, want the old dictionary's %q (an engine is replaced, never mutated)", stale, "Qm55 AV Panel")
	}
}

// Removing an entry has to work as well as adding one, or an operator can only
// ever grow the list. A cache keyed on anything but the dictionary's contents
// (a counter, a length, a "dirty" flag) gets this wrong in one direction or the
// other.
func TestEngineCacheFollowsADictionaryBackwards(t *testing.T) {
	var c label.EngineCache
	c.For([]string{"AV", "QM55"})
	reverted := c.For([]string{"AV"})
	if got := renderWith(t, reverted, "{{.A | title}}", label.Data{"A": "qm55"}); got != "Qm55" {
		t.Fatalf("after removing QM55, title = %q, want %q", got, "Qm55")
	}
}

// The zero value is the engine with no dictionary, so a caller that has not
// resolved a setting yet still renders rather than panicking.
func TestEngineCacheZeroValueTitlesWithNoDictionary(t *testing.T) {
	var c label.EngineCache
	if got := renderWith(t, c.For(nil), "{{.A | title}}", label.Data{"A": "av"}); got != "Av" {
		t.Fatalf("empty dictionary title = %q, want %q", got, "Av")
	}
}

// One cache is shared by every request-path render on the process, so concurrent
// use is the normal case, not an edge one. Run under -race.
func TestEngineCacheIsSafeForConcurrentUse(t *testing.T) {
	var c label.EngineCache
	dicts := [][]string{{"AV"}, {"AV", "DSP"}, {"DSP"}, nil}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := c.For(dicts[i%len(dicts)])
			if _, err := e.Parse("{{.A | title}}"); err != nil {
				t.Errorf("parse under concurrency: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
