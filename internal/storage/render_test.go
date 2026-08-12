package storage

import "testing"

// The acceptance example from #627 Task 15's brief: dash-joining the RAW
// segments of a component address would carry the $comp accessor straight
// into the render ("boi-17c-216b-$comp-display-1"), which is not what an
// operator wants on a cable tag or a asset label. RenderDash strips it.
func TestRenderDashStripsAccessor(t *testing.T) {
	got := RenderDash(parseOrFail(t, "boi.17c.216b.$comp.display-1"))
	want := "boi-17c-216b-display-1"
	if got != want {
		t.Fatalf("RenderDash = %q, want %q", got, want)
	}
}

func TestRenderDashSystemAccessor(t *testing.T) {
	got := RenderDash(parseOrFail(t, "boi.17c.$sys.av"))
	want := "boi-17c-av"
	if got != want {
		t.Fatalf("RenderDash = %q, want %q", got, want)
	}
}

func TestRenderDashUnplacedOpensOnAccessor(t *testing.T) {
	// Root is empty (an unplaced/orphan component): the accessor is still the
	// first segment and still stripped, leaving just the component's own
	// tail.
	got := RenderDash(parseOrFail(t, "$comp.display-1"))
	want := "display-1"
	if got != want {
		t.Fatalf("RenderDash = %q, want %q", got, want)
	}
}

func TestRenderDashPlainLocationHasNoAccessorToStrip(t *testing.T) {
	got := RenderDash(parseOrFail(t, "boi.17c.416a"))
	want := "boi-17c-416a"
	if got != want {
		t.Fatalf("RenderDash = %q, want %q", got, want)
	}
}

// RenderBare compacts the dash render further: the final segment of a
// platform-named row becomes <abbrev><ordinal>, and everything concatenates
// with no separator at all, which is the point of a "bare" form (a physical
// label with no room for punctuation). The ordinal is the one the generator
// STORED on the row (#681), so the render reads a fact rather than
// reconstructing one from the name it is about to replace.
func TestRenderBareReplacesStemOrdinalWithAbbrev(t *testing.T) {
	got := RenderBare(parseOrFail(t, "boi.17c.216b.$comp.display-1"), ordp(1), "dsp")
	want := "boi17c216bdsp1"
	if got != want {
		t.Fatalf("RenderBare = %q, want %q", got, want)
	}
}

func TestRenderBareMultiDigitOrdinal(t *testing.T) {
	got := RenderBare(parseOrFail(t, "boi.$comp.display-12"), ordp(12), "dsp")
	want := "boidsp12"
	if got != want {
		t.Fatalf("RenderBare = %q, want %q", got, want)
	}
}

// No abbrev (system and location addresses have no type-level abbreviation
// today; a component whose type carries none either) means there is nothing
// to compact the final segment TO, so RenderBare falls back to the dash
// render's segments, just concatenated instead of dashed.
func TestRenderBareNoAbbrevFallsBackToConcatenation(t *testing.T) {
	got := RenderBare(parseOrFail(t, "boi.17c.216b.$comp.display-1"), ordp(1), "")
	want := "boi17c216bdisplay-1"
	if got != want {
		t.Fatalf("RenderBare = %q, want %q", got, want)
	}
}

// An operator-renamed component (RenameComponent clears name_generated and
// nulls the ordinal in the same statement) has no ordinal the platform owns,
// so there is nothing to substitute and the segment is left exactly as the
// dash render has it.
func TestRenderBareNoOrdinalTailLeavesSegmentUnchanged(t *testing.T) {
	got := RenderBare(parseOrFail(t, "boi.$comp.front-desk-display"), nil, "dsp")
	want := "boifront-desk-display"
	if got != want {
		t.Fatalf("RenderBare = %q, want %q", got, want)
	}
}

// The #654 guarantee, now structural. An operator who renames a component
// takes the pen for good (name_generated clears and never returns except
// through :resetName, and the stored ordinal goes with it), and the name they
// chose can end in a digit run for reasons of their own: rack-3, booth-2,
// row-14. Compacting "rack-3" to "dsp3" puts a word on a cable label that
// appears nowhere in the entity's name. A NULL ordinal cannot be compacted, so
// the render no longer has to recognise the case, it simply has no number.
func TestRenderBareLeavesAForeignStemAlone(t *testing.T) {
	got := RenderBare(parseOrFail(t, "boi.$comp.rack-3"), nil, "dsp")
	want := "boirack-3"
	if got != want {
		t.Fatalf("RenderBare = %q, want %q", got, want)
	}
}

// A row whose name merely LOOKS generated is still left alone when no ordinal
// is stored for it: a hand-typed "display-1", or a row that predates the
// ordinal column and the backfill could not read a number off. The digits in
// the name are not evidence, the column is.
func TestRenderBareNoStoredOrdinalLeavesSegmentUnchanged(t *testing.T) {
	got := RenderBare(parseOrFail(t, "boi.$comp.display-1"), nil, "dsp")
	want := "boidisplay-1"
	if got != want {
		t.Fatalf("RenderBare = %q, want %q", got, want)
	}
}

func TestRenderBareEmptySegments(t *testing.T) {
	if got := RenderBare(nil, ordp(1), "dsp"); got != "" {
		t.Fatalf("RenderBare(nil) = %q, want empty", got)
	}
}

// ordp is the stored-ordinal pointer these renders take: a row the platform
// named carries a number, a row an operator named carries nil.
func ordp(n int) *int { return &n }

func TestRenderDashEmptySegments(t *testing.T) {
	if got := RenderDash(nil); got != "" {
		t.Fatalf("RenderDash(nil) = %q, want empty", got)
	}
}

// parseOrFail turns a dotted ref into the segment slice RenderDash/RenderBare
// consume: Root, then the accessor literal (path.go's own constants) when the
// address switches plane, then Tail. This is exactly what PathOf builds from
// the database in production; here it comes from ParseAddress so the render
// tests carry no database dependency at all.
func parseOrFail(t *testing.T, ref string) []string {
	t.Helper()
	addr, err := ParseAddress(ref)
	if err != nil {
		t.Fatalf("ParseAddress(%q): %v", ref, err)
	}
	if addr == nil {
		t.Fatalf("ParseAddress(%q) = nil, want an address", ref)
	}
	segs := append([]string{}, addr.Root...)
	switch addr.Kind {
	case AddressComponent:
		segs = append(segs, accessorComp)
	case AddressSystem:
		segs = append(segs, accessorSys)
	}
	segs = append(segs, addr.Tail...)
	return segs
}
