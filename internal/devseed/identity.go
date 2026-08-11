package devseed

import (
	"context"
	"fmt"
	"sort"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// How a fixture row is identified, and why it cannot be by name.
//
// The fixture keys everything on `key`, a string local to the document that is
// never written to the database, because most of this estate does not know its
// own name: a row with no `name` asks the platform to mint one (#657), and the
// platform picks it from the row's classification and the lowest free ordinal in
// its placement bucket. So a reference (`location: boardroom`, `component:
// panel-a`) resolves through the key to a row ID, and every gateway call this
// package makes passes that ID rather than a name (a uuid is a legal reference
// everywhere a name is, ADR-0062). That also settles the ambiguity a generated
// name creates on purpose: three rooms each hold a `display-1`, so a bare name
// would be a refusal listing three candidates.
//
// The second half of the problem is recognising a row on a LATER run, which is
// what keeps `make dev` from stacking a second estate on every start. A row the
// fixture names is found by its name, the way it always was. A row the PLATFORM
// named is found by its position: among the platform-named rows of the same
// classification in the same bucket, in the order the generator allocated them,
// the fixture's k-th such row is the estate's k-th. That is the fixture's real
// claim anyway. It does not say "there is a display called display-2 in the
// boardroom", it says "the boardroom holds three displays", and the seed's job
// is to make the estate match.
//
// Two consequences worth stating rather than discovering:
//
//   - Only platform-named rows count. A component an operator adds to the
//     boardroom by hand is not one of the fixture's rows and never stands in for
//     one, which is what the name_generated filter in the callers is for.
//   - The classification has to partition the ordinal space. Two DIFFERENT
//     products classified under one component_type would share a stem and
//     therefore one run of ordinals, and grouping by product would then zip the
//     fixture onto the estate in the wrong order. No such pair is in the
//     fixture, and devseed_test asserts every seeded name by value, so a pair
//     added later fails loudly rather than silently mis-assigning.

// genSlot is one ordinal space: a placement bucket and the classification whose
// stem is minted into it.
type genSlot struct {
	bucket string
	class  string
}

// genRow is one platform-named row already in the estate.
type genRow struct {
	slot    genSlot
	ordinal int
	id      string
}

// genIndex hands a fixture row the estate row that already stands for it, or
// nothing when the estate is short of one and the caller must create it.
type genIndex struct {
	rows map[genSlot][]string
	next map[genSlot]int
}

// newGenIndex groups the estate's platform-named rows into their ordinal spaces,
// each in allocation order. Sorting on the stored ordinal rather than on the
// name is the same choice ADR-0097 makes for allocation itself: the number is a
// fact the platform recorded, and picking it back out of a name would have to
// know the mint's shape, which a suppressed first ordinal ("boardroom" is
// ordinal 1) makes unrecoverable.
func newGenIndex(rows []genRow) *genIndex {
	sorted := make([]genRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ordinal < sorted[j].ordinal })
	g := &genIndex{rows: map[genSlot][]string{}, next: map[genSlot]int{}}
	for _, r := range sorted {
		g.rows[r.slot] = append(g.rows[r.slot], r.id)
	}
	return g
}

// take claims this slot's next row for the fixture row being seeded, returning
// its id, or "" when the estate holds fewer rows in this slot than the fixture
// declares. Every call advances the slot, so two fixture rows in one slot never
// resolve to the same estate row.
func (g *genIndex) take(slot genSlot) string {
	k := g.next[slot]
	g.next[slot]++
	if k < len(g.rows[slot]) {
		return g.rows[slot][k]
	}
	return ""
}

// grew records a row this run created, so a later fixture row in the same slot
// sees the estate as it now is rather than as it was when the index was built.
func (g *genIndex) grew(slot genSlot, id string) {
	g.rows[slot] = append(g.rows[slot], id)
}

// bucketOf is the placement bucket a generated name is unique within, in the
// order the scoped-name unique indexes resolve it: a parent wins over a
// location, and a row with neither is unplaced. It takes the resolved ids, so a
// tree with no located-at column (a location) simply passes "" for the second.
func bucketOf(parentID, locationID string) string {
	switch {
	case parentID != "":
		return "parent/" + parentID
	case locationID != "":
		return "location/" + locationID
	default:
		return "unplaced"
	}
}

// productClass is the classification a component's ordinal space is grouped by.
// An omitted product is not "no classification": the gateway resolves it to the
// generic device, which is what the row then reports and what its name is minted
// from, so the fixture and the estate have to spell it the same way.
func productClass(product string) string {
	if product == "" {
		return "generic-device"
	}
	return product
}

// existingRowID answers "is this fixture row already in the estate", by the one
// question that fits it. A row the fixture NAMES is looked up by name, the way
// every seed loop here always has. A row the PLATFORM names has no name for the
// fixture to ask about, so it claims the next row of its own classification in
// its own bucket, and an empty answer means the estate is short of one and the
// caller must create it.
//
// The two are one function rather than two branches at each of the three call
// sites, because the choice is a property of the fixture row (does it carry a
// name) and not of the entity kind, and a call site that got it backwards would
// either duplicate the estate on every start or silently stop exercising the
// generator.
func existingRowID(ctx context.Context, name, key string, slot genSlot, idx *genIndex, byName func(string) (string, error)) (string, error) {
	if name != "" {
		id, err := byName(name)
		if err != nil {
			return "", err
		}
		return id, nil
	}
	if key == "" {
		return "", fmt.Errorf("devseed: a fixture row with no name needs a key")
	}
	return idx.take(slot), nil
}

// locationGenIndex reads the platform-named locations already in the estate,
// grouped into the ordinal space each was allocated in: the parent bucket (a
// location has no located-at column, so its buckets are two, not three) and the
// location_type whose name rule minted it.
func locationGenIndex(ctx context.Context, gw storage.Gateway, read scope.Set) (*genIndex, error) {
	rows, err := gw.ListLocations(ctx, read)
	if err != nil {
		return nil, fmt.Errorf("devseed: list locations: %w", err)
	}
	var gen []genRow
	for _, l := range rows {
		if !l.NameGenerated || l.Ordinal == nil {
			continue
		}
		gen = append(gen, genRow{
			slot:    genSlot{bucket: bucketOf(strOrEmpty(l.ParentID), ""), class: l.LocationType},
			ordinal: *l.Ordinal,
			id:      l.ID,
		})
	}
	return newGenIndex(gen), nil
}

// componentGenIndex is locationGenIndex on the component tier: the placement
// bucket, and the product whose component_type stem the name was minted from.
func componentGenIndex(ctx context.Context, gw storage.Gateway, read scope.Set) (*genIndex, error) {
	rows, err := gw.ListComponents(ctx, read)
	if err != nil {
		return nil, fmt.Errorf("devseed: list components: %w", err)
	}
	var gen []genRow
	for _, c := range rows {
		if !c.NameGenerated || c.Ordinal == nil {
			continue
		}
		gen = append(gen, genRow{
			slot:    genSlot{bucket: bucketOf(strOrEmpty(c.ParentID), strOrEmpty(c.LocationID)), class: strOrEmpty(c.ProductHandle)},
			ordinal: *c.Ordinal,
			id:      c.ID,
		})
	}
	return newGenIndex(gen), nil
}

// systemGenIndex is componentGenIndex on the system tier.
func systemGenIndex(ctx context.Context, gw storage.Gateway, read scope.Set) (*genIndex, error) {
	rows, err := gw.ListSystems(ctx, read)
	if err != nil {
		return nil, fmt.Errorf("devseed: list systems: %w", err)
	}
	var gen []genRow
	for _, s := range rows {
		if !s.NameGenerated || s.Ordinal == nil {
			continue
		}
		gen = append(gen, genRow{
			slot:    genSlot{bucket: bucketOf(strOrEmpty(s.ParentID), strOrEmpty(s.LocationID)), class: strOrEmpty(s.SystemTypeName)},
			ordinal: *s.Ordinal,
			id:      s.ID,
		})
	}
	return newGenIndex(gen), nil
}

// memberEnds resolves a membership's two fixture keys to the ids the gateway is
// handed. Both ends by id, never by name: three rooms in this estate each hold a
// component called display-1, so a bare name is a refusal listing candidates
// rather than a lookup.
func memberEnds(sysIDs, compIDs map[string]string, systemKey, componentKey string) (string, string, error) {
	sysID, ok := sysIDs[systemKey]
	if !ok {
		return "", "", fmt.Errorf("devseed: membership references unknown system key %q", systemKey)
	}
	compID, ok := compIDs[componentKey]
	if !ok {
		return "", "", fmt.Errorf("devseed: membership references unknown component key %q", componentKey)
	}
	return sysID, compID, nil
}

// strOrEmpty reads a nullable reference column as the empty string, the one
// spelling the bucket and class keys above understand.
func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
