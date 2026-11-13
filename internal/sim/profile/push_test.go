package profile

import (
	"strings"
	"testing"
	"testing/fstest"
)

const pusherProfile = `
name: pusher-a
manufacturer: Test
model: Pusher
device_type: cloud-service
slug_prefix: pusher
interfaces: []
push:
  every: %EVERY%
  body: %BODY%
`

func pushStore(every, body string) (*Store, error) {
	src := strings.NewReplacer("%EVERY%", every, "%BODY%", body).Replace(pusherProfile)
	return LoadStoreFS(fstest.MapFS{"devices/pusher-a.yaml": {Data: []byte(src)}})
}

// A profile's push face (#1743) loads with its cadence and body template,
// and one whose cadence or body could never push is refused at load.
func TestPushFaceLoadsAndIsValidated(t *testing.T) {
	store, err := pushStore("30s", `'{"owner":{"kind":"system","ref":"{{ .Owner }}"}}'`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	p := store.Devices["pusher-a"].Push
	if p == nil || p.Every != "30s" || !strings.Contains(p.Body, ".Owner") {
		t.Fatalf("push face = %+v, want every 30s and the body", p)
	}
	for _, c := range []struct{ every, body, want string }{
		{"soon", `'{}'`, "every"},
		{"0s", `'{}'`, "every"},
		{"500ms", `'{}'`, "every"},
		{"30s", `''`, "body"},
	} {
		if _, err := pushStore(c.every, c.body); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("every %q body %s: err %v, want one naming %s", c.every, c.body, err, c.want)
		}
	}
}

// The shipped booking service is a push face: it pushes on a cadence and
// declares no webhook, which is a different face.
func TestTheShippedBookingServiceHasAPushFace(t *testing.T) {
	store, err := LoadStore(findProfilesRoot(t))
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	p, ok := store.Devices["booking-service"]
	if !ok {
		t.Fatal("no booking-service profile")
	}
	if p.Push == nil || p.Webhook != nil || p.DeviceType != "cloud-service" {
		t.Errorf("booking-service: push %+v webhook %+v type %s, want a cloud service with a push face and no webhook", p.Push, p.Webhook, p.DeviceType)
	}
}
