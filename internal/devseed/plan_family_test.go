package devseed

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// The dev seed plans a Go family's component exactly as it plans the YAML
// twin's (#2829): the same address and endpoint for each product.
func TestPlanDevicesPlansAFamilyProductAsItPlansItsYAMLTwin(t *testing.T) {
	t.Parallel()
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range spectest.Pairs {
		plan := func(product string) []plannedEndpoint {
			got, err := planDevicesOver(cat, planRows{Components: []componentRow{{Key: "c", Product: product}}}, nil, 10000)
			if err != nil {
				t.Fatal(err)
			}
			return got
		}
		g, y := plan(p.Go), plan(p.YAML)
		if len(g) != 1 || g[0].Transport != "http" {
			t.Fatalf("%s: plan = %+v, want one http probe", p.Go, g)
		}
		if !reflect.DeepEqual(g, y) {
			t.Errorf("%s: plan = %+v, twin %+v", p.Go, g, y)
		}
	}
}

// A Go family's product that declares no webhook is refused as a sender the way
// the YAML twin's is: a sender is a profile with a named webhook driver.
func TestSenderPlansRefuseAFamilyProductAsTheyRefuseItsYAMLTwin(t *testing.T) {
	t.Parallel()
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range spectest.Pairs {
		for _, product := range []string{p.Go, p.YAML} {
			rows := planRows{Components: []componentRow{{Key: "c", Product: product}}, Senders: []senderRow{{Comp: 0}}}
			_, err := senderPlans(rows, cat.ProfileFor, nil)
			if err == nil || !strings.Contains(err.Error(), "names no webhook driver") {
				t.Errorf("%s: err = %v, want a refusal for no webhook driver", product, err)
			}
		}
	}
}
