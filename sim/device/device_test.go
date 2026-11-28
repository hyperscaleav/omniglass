package device

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// The shipped catalog holds the embedded YAML profiles and every registered
// family, and a registered family's products resolve to it.
func TestCatalogHoldsTheShippedProfilesAndEveryFamily(t *testing.T) {
	cat, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	r, ok := cat.ForProduct("polaris-dsp-16")
	if !ok || r.Definition.Kind() != spec.KindYAML {
		t.Fatalf("polaris-dsp-16 resolved to %+v (ok=%v), want its YAML profile", r, ok)
	}
	for _, fam := range Families() {
		d, ok := cat.Lookup(fam.Name)
		if !ok || d.Kind() != spec.KindGo {
			t.Errorf("family %s is not in the catalog as a Go definition", fam.Name)
		}
		for _, m := range fam.Models {
			if r, ok := cat.ForProduct(m.Product); !ok || r.Definition.ID() != fam.Name {
				t.Errorf("product %s did not resolve to family %s", m.Product, fam.Name)
			}
		}
	}
}

// A family's webhook subscribe route (#1370) is a POST route it declares on an
// http interface, so the simulator's subscribe call reaches the route that points
// the service's posts at a receiver, and a provision route it names (#1747) is a
// POST route too. Every family that names a webhook face declares both.
func TestEveryWebhookFaceNamesItsRoutes(t *testing.T) {
	senders := 0
	for _, fam := range Families() {
		w := fam.Webhook
		if w == nil {
			continue
		}
		senders++
		routes := map[string]bool{}
		for _, r := range fam.HTTP {
			routes[r.Match] = true
		}
		if !routes["POST "+w.Subscribe] {
			t.Errorf("%s webhook.subscribe %q names no POST route", fam.Name, w.Subscribe)
		}
		if w.Provision != "" && !routes["POST "+w.Provision] {
			t.Errorf("%s webhook.provision %q names no POST route, so the simulator cannot add a bound room", fam.Name, w.Provision)
		}
	}
	if senders == 0 {
		t.Fatal("no family names a webhook face; the check checked nothing")
	}
}

// The lab shows one UCC fan-out shape (#1390): the account-wide conduit. The
// per-room ucc-service is retired, and exactly one ucc definition ships.
func TestTheLabShowsOneUCCFanOutShape(t *testing.T) {
	cat, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cat.Lookup("ucc-service"); ok {
		t.Error("the ucc-service definition still loads (#1390)")
	}
	if _, ok := cat.ForProduct("ucc-service"); ok {
		t.Error("a definition still serves the ucc-service product (#1390)")
	}
	ucc := 0
	for _, def := range cat.Definitions() {
		if strings.HasPrefix(def.ID(), "ucc-") {
			ucc++
		}
	}
	if ucc != 1 {
		t.Errorf("%d ucc definitions load, want the one account-wide conduit", ucc)
	}
}
