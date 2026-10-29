package profiles

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// A profile's webhook subscribe route (#1370) is a POST route it declares on
// an http interface, so the simulator's subscribe call reaches the mutations
// that point the service's posts at a receiver. Every profile that posts
// webhooks (an emit whose url reads the configured target) declares one.
func TestEveryWebhookSenderDeclaresItsSubscribeRoute(t *testing.T) {
	store, err := profile.LoadStoreFS(FS)
	if err != nil {
		t.Fatal(err)
	}
	senders := 0
	for name, p := range store.Devices {
		posts := false
		routes := map[string]bool{}
		for _, iface := range p.Interfaces {
			if iface.Protocol != "http" {
				continue
			}
			for _, r := range iface.Routes {
				routes[r.Match] = true
				if r.Emit != nil {
					posts = true
				}
			}
		}
		for _, b := range p.Behaviors {
			if b.Emit != nil {
				posts = true
			}
		}
		if posts {
			senders++
		}
		switch {
		case p.Webhook == nil && posts:
			t.Errorf("%s posts webhooks but declares no webhook.subscribe route, so the simulator cannot point it at its endpoint", name)
		case p.Webhook != nil && !routes["POST "+p.Webhook.Subscribe]:
			t.Errorf("%s webhook.subscribe %q names no POST route on its http interfaces", name, p.Webhook.Subscribe)
		}
	}
	if senders == 0 {
		t.Fatal("no profile posts webhooks; the check checked nothing")
	}
}
