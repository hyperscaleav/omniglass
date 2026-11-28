package devseed

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// The dev seed plans a Go family's component (#2829): an address and an http
// endpoint for each product.
func TestPlanDevicesPlansAFamilyProduct(t *testing.T) {
	t.Parallel()
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, product := range spectest.Products {
		got, err := planDevicesOver(cat, planRows{Components: []componentRow{{Key: "c", Product: product}}}, nil, 10000)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Transport != "http" {
			t.Fatalf("%s: plan = %+v, want one http probe", product, got)
		}
	}
}

// A family's product that declares no webhook is refused as a sender: a sender
// is a family with a named webhook driver.
func TestSenderPlansRefuseAFamilyProductWithNoWebhook(t *testing.T) {
	t.Parallel()
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, product := range spectest.Products {
		rows := planRows{Components: []componentRow{{Key: "c", Product: product}}, Senders: []senderRow{{Comp: 0}}}
		_, err := senderPlans(rows, cat.ProfileFor, nil)
		if err == nil || !strings.Contains(err.Error(), "names no webhook driver") {
			t.Errorf("%s: err = %v, want a refusal for no webhook driver", product, err)
		}
	}
}
