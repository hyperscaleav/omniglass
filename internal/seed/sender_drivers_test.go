package seed

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// A simulated service a scale lab seeds as a webhook sender (#1747) names
// the shipped driver whose endpoint receives its posts. That driver is a
// webhook driver with a credential input to mint per run and exactly one
// room identity, so the lab can seed the endpoint, its signing key and one
// binding per room from the profile alone.
func TestEverySimulatedWebhookSenderNamesAShippedWebhookDriver(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	named := 0
	for name, p := range store.Devices {
		if p.Webhook == nil || p.Webhook.Driver == "" {
			continue
		}
		named++
		sp := shippedSpec(t, p.Webhook.Driver)
		if sp.Transport != "webhook" {
			t.Errorf("%s names driver %s, transport %q, want webhook", name, p.Webhook.Driver, sp.Transport)
		}
		if sp.Session == nil || sp.Session.Credential == "" {
			t.Errorf("%s names driver %s, which declares no session credential to mint", name, p.Webhook.Driver)
		}
		keys := map[string]bool{}
		for _, l := range sp.Listeners {
			if l.Identity != nil {
				keys[l.Identity.Key] = true
			}
		}
		if len(keys) != 1 {
			t.Errorf("%s names driver %s with identity keys %v, want exactly one room identity", name, p.Webhook.Driver, keys)
		}
		if p.Webhook.Provision == "" {
			t.Errorf("%s names a driver but no provision route, so its account never learns the bound rooms", name)
		}
	}
	if named == 0 {
		t.Fatal("no simulated service names a webhook driver; the check checked nothing")
	}
}
