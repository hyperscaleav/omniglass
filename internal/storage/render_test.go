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

// RenderBare compacts the dash render further: the final stem-ordinal segment
// becomes <abbrev><ordinal>, and everything concatenates with no separator at
// all, which is the point of a "bare" form (a physical label with no room for
// punctuation).
func TestRenderBareReplacesStemOrdinalWithAbbrev(t *testing.T) {
	got := RenderBare(parseOrFail(t, "boi.17c.216b.$comp.display-1"), "dsp")
	want := "boi17c216bdsp1"
	if got != want {
		t.Fatalf("RenderBare = %q, want %q", got, want)
	}
}

func TestRenderBareMultiDigitOrdinal(t *testing.T) {
	got := RenderBare(parseOrFail(t, "boi.$comp.display-12"), "dsp")
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
	got := RenderBare(parseOrFail(t, "boi.17c.216b.$comp.display-1"), "")
	want := "boi17c216bdisplay-1"
	if got != want {
		t.Fatalf("RenderBare = %q, want %q", got, want)
	}
}

// An operator-renamed component (RenameComponent clears name_generated
// forever) may carry no trailing ordinal at all: "front-desk-display" has
// nothing pickOrdinal-shaped for RenderBare to substitute, so the segment is
// left exactly as the dash render has it.
func TestRenderBareNoOrdinalTailLeavesSegmentUnchanged(t *testing.T) {
	got := RenderBare(parseOrFail(t, "boi.$comp.front-desk-display"), "dsp")
	want := "boifront-desk-display"
	if got != want {
		t.Fatalf("RenderBare = %q, want %q", got, want)
	}
}

// The abbrev substitution belongs to the type that minted the name, so it may
// only fire on that type's own stem. An operator who renames a component takes
// the pen for good (name_generated clears and never returns except through
// :resetName), and the name they chose can end in a digit run for reasons of
// their own: rack-3, booth-2, row-14. Compacting "rack-3" to "dsp3" puts a word
// on a cable label that appears nowhere in the entity's name.
func TestRenderBareLeavesAForeignStemAlone(t *testing.T) {
	got := RenderBare(parseOrFail(t, "boi.$comp.rack-3"), "dsp")
	want := "boirack3"
	if got != want {
		t.Fatalf("RenderBare = %q, want %q", got, want)
	}
}

// A stem the caller could not resolve is not a licence to substitute on any
// trailing digit run: with nothing to match against, the segment stands.
func TestRenderBareEmptyStemLeavesSegmentUnchanged(t *testing.T) {
	got := RenderBare(parseOrFail(t, "boi.$comp.display-1"), "dsp")
	want := "boidisplay-1"
	if got != want {
		t.Fatalf("RenderBare = %q, want %q", got, want)
	}
}

func TestRenderBareEmptySegments(t *testing.T) {
	if got := RenderBare(nil, "dsp"); got != "" {
		t.Fatalf("RenderBare(nil) = %q, want empty", got)
	}
}

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
