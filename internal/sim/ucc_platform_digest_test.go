//go:build linux

package sim

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The UCC platform posts its batched rooms report on its own schedule
// (#1371): once subscribed, its digest behavior posts rooms.state naming every
// room on the account, so its webhook endpoint hears from it with nobody
// firing a route. Unsubscribed, it posts nothing.
func TestTheUCCPlatformPostsItsDigestOnItsOwnSchedule(t *testing.T) {
	got := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		m["header"] = r.Header.Get("X-UCC-Event")
		got <- m
	}))
	defer srv.Close()
	e := newCatalogEngine(t)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "platform", Hostname: "platform", LoopbackIP: "127.88.9.31", ProfileName: "ucc-platform", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	digest := -1
	for i := range d.behaviors {
		if d.behaviors[i].Name == "rooms_report" {
			digest = i
		}
	}
	if digest < 0 {
		t.Fatal("the platform declares no rooms_report behavior")
	}

	d.tickBehavior(&d.behaviors[digest])
	select {
	case m := <-got:
		t.Fatalf("an unsubscribed platform posted %v", m)
	case <-time.After(300 * time.Millisecond):
	}

	if res := d.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/webhooks",
		Body: `{"url":"` + srv.URL + `/hook","scheme":"hmac","secret":"k"}`}); res.Status != 200 {
		t.Fatalf("subscribe: %+v", res)
	}
	d.tickBehavior(&d.behaviors[digest])
	select {
	case m := <-got:
		rooms, _ := m["rooms"].([]any)
		if m["header"] != "rooms.state" || m["event"] != "rooms.state" || len(rooms) == 0 {
			t.Fatalf("the digest = %v, want rooms.state naming the account's rooms", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the subscribed platform's digest never posted")
	}
}
