package devseed_test

import (
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

// The plan provisions one webhook endpoint and one signing secret for the
// fleet's one meeting platform (#1747, #2790): the profile's named driver, the
// driver's session credential as the input that takes the secret, a secret of
// that input's type and of the platform's own name. The platform is also a
// polled service, so its ids (rooms and devices) are bound by the service seed
// and the sender binds none.
func TestThePlanProvisionsOneWebhookAndSecretForTheMeetingPlatform(t *testing.T) {
	t.Parallel()
	p := corporatePlan(t, 20)
	wantSite, _ := senderSites(p)
	if len(wantSite) != 1 {
		t.Fatalf("the corporate plan declares %d webhook senders, want the fleet's one meeting platform", len(wantSite))
	}
	got, err := devseed.PlanSenders(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%d senders planned, want one", len(got))
	}
	s := got[0]
	if site, ok := wantSite[s.Component]; !ok || site != p.Sites[0].Name {
		t.Errorf("planned sender %s is not the headquarters' platform %v", s.Component, wantSite)
	}
	if s.Driver != "ucc-webhook" || s.Input != "signing-key" || s.SecretType != "hmac-key" {
		t.Errorf("%s: driver %s input %s type %s, want ucc-webhook signing-key hmac-key", s.Component, s.Driver, s.Input, s.SecretType)
	}
	if want := s.Component + "-signing"; s.Secret != want {
		t.Errorf("%s signs with secret %q, want %q", s.Component, s.Secret, want)
	}
	if s.Key != "" || len(s.Systems) != 0 {
		t.Errorf("%s binds %d systems on %q itself, want none (the polled service seeds its ids)", s.Component, len(s.Systems), s.Key)
	}
}
