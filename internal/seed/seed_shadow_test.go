package seed_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// unreferenced reports a shipped name nothing else in the seed names (its own
// entry is the one occurrence), so a test that sets an operator's row against it
// touches no shipped rule, product or standard.
func unreferenced(facts []byte, name string) bool {
	return strings.Count(string(facts), `"`+name+`"`) == 1
}

// A release can ship a name an operator already holds in another registry
// (#1307): the operator's custom property x, then a release that seeds a metric
// x. The official row is the platform's and always lands, so the boot never
// skips it and never fails on it; on this install the operator's row shadows it
// at ingest, so their series keep landing where they did and no sample is
// refused. (The namespace tiering in #318 is the general form of the rule.)
func TestSeedAlwaysUpsertsAnOfficialRowAndAnOperatorsOfTheSameNameShadowsIt(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	raw, err := seed.FactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	var facts struct {
		MetricTypes []struct {
			Name string `json:"name"`
		} `json:"metric_types"`
	}
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	// A shipped metric an operator could name a property after: not a reserved
	// key (the node's own, closed to an operator-created type), and named
	// nowhere else in the seed.
	var clash string
	for _, m := range facts.MetricTypes {
		if _, reserved := writerclass.Reserved(m.Name); !reserved && unreferenced(raw, m.Name) {
			clash = m.Name
			break
		}
	}
	if clash == "" {
		t.Fatal("no shipped metric an operator may name that nothing else in the seed names")
	}
	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: clash, DataType: "string"}); err != nil {
		t.Fatalf("the operator's property %q: %v", clash, err)
	}

	for boot := 1; boot <= 2; boot++ {
		if err := seed.Run(ctx, gw); err != nil {
			t.Fatalf("boot %d with the operator holding a shipped name: %v", boot, err)
		}
	}

	metrics, err := gw.ListMetricTypes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	properties, err := gw.ListPropertyTypes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	events, err := gw.ListEventTypes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var official *storage.MetricType
	for i := range metrics {
		if metrics[i].Name == clash {
			official = &metrics[i]
		}
	}
	if official == nil || !official.Official {
		t.Errorf("the official metric %q = %+v, want it seeded: the platform's rows always land", clash, official)
	}
	var theirs *storage.PropertyType
	for i := range properties {
		if properties[i].Name == clash {
			theirs = &properties[i]
		}
	}
	if theirs == nil || theirs.Official {
		t.Errorf("the operator's property %q = %+v, want it untouched and still theirs", clash, theirs)
	}

	// At ingest the name resolves to the operator's row, and nothing is refused.
	reg := collection.NewRegistry(metrics, properties, events)
	if _, ok := reg.Property(clash); !ok {
		t.Errorf("%q does not resolve on the operator's property lane", clash)
	}
	if _, ok := reg.Metric(clash); ok {
		t.Errorf("%q resolves on the official metric lane, over the operator's row", clash)
	}
	if c := reg.Collisions(); len(c) != 0 {
		t.Errorf("Collisions() = %v, want none: the operator's row shadows the official one", c)
	}
	if s := reg.Shadowed(); len(s) != 1 || s[0] != clash {
		t.Errorf("Shadowed() = %v, want [%s]", s, clash)
	}
}
