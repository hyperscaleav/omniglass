package devseed_test

import (
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
)

// senderSites is each site's webhook senders (#1747) and its systems, read
// off the plan itself.
func senderSites(p *scenario.Plan) (senders map[string]string, systems map[string][]string) {
	senders, systems = map[string]string{}, map[string][]string{}
	for _, site := range p.Sites {
		for _, ps := range site.Pushers {
			if ps.Webhook {
				senders[ps.Name] = site.Name
			}
		}
		for _, s := range site.Systems {
			systems[site.Name] = append(systems[site.Name], s.Name)
		}
	}
	return senders, systems
}

// The scale plan provisions one webhook endpoint and one signing secret per
// declared platform (#1747): the profile's named driver, the driver's session
// credential as the input that takes the secret, a secret of that input's
// type and of the platform's own name, and one binding per system of its
// site on the driver's room identity.
func TestThePlanProvisionsOneWebhookAndSecretPerPlatform(t *testing.T) {
	t.Parallel()
	p := corporatePlan(t, 20)
	wantSite, systems := senderSites(p)
	if len(wantSite) != len(p.Sites) {
		t.Fatalf("the corporate plan declares %d platforms over %d sites, want one per site", len(wantSite), len(p.Sites))
	}
	got, err := devseed.PlanSenders(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(wantSite) {
		t.Fatalf("%d senders planned, want %d (one per platform)", len(got), len(wantSite))
	}
	secrets := map[string]bool{}
	for _, s := range got {
		site, ok := wantSite[s.Component]
		if !ok {
			t.Errorf("planned sender %s is no platform the plan declares", s.Component)
			continue
		}
		delete(wantSite, s.Component)
		if s.Driver != "ucc-webhook" || s.Input != "signing-key" || s.SecretType != "hmac-key" || s.Key != "ucc-room-id" {
			t.Errorf("%s: driver %s input %s type %s key %s, want ucc-webhook signing-key hmac-key ucc-room-id",
				s.Component, s.Driver, s.Input, s.SecretType, s.Key)
		}
		if want := s.Component + "-signing"; s.Secret != want {
			t.Errorf("%s signs with secret %q, want %q", s.Component, s.Secret, want)
		}
		if secrets[s.Secret] {
			t.Errorf("secret %s is shared by two platforms", s.Secret)
		}
		secrets[s.Secret] = true
		if fmt.Sprint(s.Systems) != fmt.Sprint(systems[site]) {
			t.Errorf("%s binds %v, want its site's systems %v", s.Component, s.Systems, systems[site])
		}
	}
	if len(wantSite) != 0 {
		t.Errorf("platforms with no sender planned: %v", wantSite)
	}
}
