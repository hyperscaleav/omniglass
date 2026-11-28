package httpapi

import (
	"encoding/json"
	"testing"
)

func TestStatusLayoutAndOrders(t *testing.T) {
	id := Identity{Model: "M", Serial: "S", Firmware: "F", Uptime: 7}
	type blk struct {
		A int  `json:"a"`
		B bool `json:"b"`
	}
	got := string(Status(id, UptimeFirst, Block("x", blk{1, true}), Inline("y", map[string]int{"z": 2, "a": 1})))
	want := "{\n  \"device\": {\n    \"uptime\": 7,\n    \"model\": \"M\",\n    \"serial\": \"S\",\n    \"firmware\": \"F\"\n  },\n" +
		"  \"x\": {\n    \"a\": 1,\n    \"b\": true\n  },\n  \"y\": {\"a\":1,\"z\":2}\n}\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	mf := string(Status(id, ModelFirst))
	if mf != "{\n  \"device\": {\n    \"model\": \"M\",\n    \"serial\": \"S\",\n    \"firmware\": \"F\",\n    \"uptime\": 7\n  }\n}\n" {
		t.Fatalf("model first: %q", mf)
	}
}

func TestStatusIsValidJSONAndKeepsMarkup(t *testing.T) {
	var v map[string]any
	body := Status(Identity{Model: `a"b&<c>`}, ModelFirst)
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("not JSON: %v: %s", err, body)
	}
	if v["device"].(map[string]any)["model"] != `a"b&<c>` {
		t.Fatalf("model round-trips as %v", v["device"])
	}
}

func TestStatusRouteReadsStateWhenAsked(t *testing.T) {
	n := int64(1)
	r := StatusRoute(ModelFirst, func() (Identity, []Section) { return Identity{Uptime: n}, nil })
	a := string(r.Serve().Body)
	n = 2
	if b := string(r.Serve().Body); a == b || r.Method != "GET" || r.Path != "/api/status" || r.Serve().ContentType != "application/json" {
		t.Fatalf("route %s %s serves a stale document", r.Method, r.Path)
	}
}

func TestSectionsLaysOutTopLevelKeysAsStatusLaysOutWhatFollowsItsIdentity(t *testing.T) {
	type pair struct {
		A int  `json:"a"`
		B bool `json:"b"`
	}
	got := string(Sections(Inline("count", 8), Block("pair", pair{1, true}), Inline("list", []pair{{2, false}})))
	want := "{\n  \"count\": 8,\n  \"pair\": {\n    \"a\": 1,\n    \"b\": true\n  },\n  \"list\": [{\"a\":2,\"b\":false}]\n}\n"
	if got != want {
		t.Fatalf("Sections = %q, want %q", got, want)
	}
}
