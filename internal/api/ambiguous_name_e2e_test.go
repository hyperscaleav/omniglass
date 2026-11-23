package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// ambiguousFixture is the fleet #1134's repro names: a display-1 in each of
// three rooms of one campus, each with an https endpoint.
type ambiguousFixture struct {
	c        *apiClient
	dsn      string
	owner    string
	compIDs  map[string]string // room name -> display-1 uuid
	compAddr map[string]string // room name -> display-1 dotted address
}

func newAmbiguousFixture(t *testing.T) *ambiguousFixture {
	t.Helper()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	owner, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	f := &ambiguousFixture{
		c: &apiClient{t: t, ctx: ctx, base: srv.URL}, dsn: dsn, owner: owner,
		compIDs: map[string]string{}, compAddr: map[string]string{},
	}
	f.c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "campus", "location_type": "campus"}, http.StatusCreated)
	for _, room := range []string{"room-a", "room-b", "room-c"} {
		f.c.do(owner, http.MethodPost, "/locations", map[string]any{"name": room, "location_type": "room", "parent": "campus"}, http.StatusCreated)
		var comp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(f.c.do(owner, http.MethodPost, "/components", map[string]any{
			"name": "display-1", "product": "generic-device", "location": room,
		}, http.StatusCreated), &comp); err != nil {
			t.Fatalf("decode display-1 in %s: %v", room, err)
		}
		f.compIDs[room] = comp.ID
		f.compAddr[room] = "campus." + room + ".$comp.display-1"
		f.c.do(owner, http.MethodPost, "/endpoints", map[string]any{"transport": "https", "component": f.compAddr[room]}, http.StatusCreated)
	}
	return f
}

// problemDetail is the problem body's human sentence.
func problemDetail(t *testing.T, body []byte) string {
	t.Helper()
	var p struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("decode problem body %s: %v", body, err)
	}
	return p.Detail
}

// candidatePattern matches one "<address> (<uuid>)" spelling of a tree row.
var candidatePattern = regexp.MustCompile(`([^\s,]+) \(([0-9a-f-]{36})\)`)

// TestAmbiguousNameIs409OnEveryCollection pins #1134: a name that answers to
// more than one row the caller can see is the same refusal on a tree entity as
// on an endpoint, a 409 naming the reference, the count and each candidate in a
// spelling that resolves.
func TestAmbiguousNameIs409OnEveryCollection(t *testing.T) {
	t.Parallel()
	f := newAmbiguousFixture(t)

	status, body := f.c.send(f.owner, http.MethodGet, "/components/display-1", nil)
	if status != http.StatusConflict {
		t.Fatalf("GET /components/display-1 = %d, want 409\nbody: %s", status, body)
	}
	msg := problemDetail(t, body)
	if !strings.Contains(msg, `"display-1"`) || !strings.Contains(msg, "3 you can see answer to it") {
		t.Errorf("component 404 = %q, want it to name display-1 and the count", msg)
	}
	for _, room := range []string{"room-a", "room-b", "room-c"} {
		want := fmt.Sprintf("%s (%s)", f.compAddr[room], f.compIDs[room])
		if !strings.Contains(msg, want) {
			t.Errorf("component 404 = %q, want candidate %q", msg, want)
		}
	}

	status, body = f.c.send(f.owner, http.MethodGet, "/endpoints/display-1%2Fhttps", nil)
	if status != http.StatusConflict {
		t.Fatalf("GET /endpoints/display-1%%2Fhttps = %d, want 409\nbody: %s", status, body)
	}
	epMsg := problemDetail(t, body)
	if !strings.Contains(epMsg, "3 you can see answer to it") {
		t.Errorf("endpoint 404 = %q, want the same count phrasing as the component's", epMsg)
	}
	for _, room := range []string{"room-a", "room-b", "room-c"} {
		if !strings.Contains(epMsg, f.compAddr[room]+"/https") {
			t.Errorf("endpoint 404 = %q, want candidate %q", epMsg, f.compAddr[room]+"/https")
		}
	}
}

// TestAmbiguousNameNeverListsWhatTheCallerCannotRead pins the redaction: the
// candidates are built after the scope filter, so a row outside the caller's
// read scope is neither listed nor counted.
func TestAmbiguousNameNeverListsWhatTheCallerCannotRead(t *testing.T) {
	t.Parallel()
	f := newAmbiguousFixture(t)
	tok := setupScopedUser(t, context.Background(), f.dsn, "two-rooms",
		grantSpec{role: "deploy", scopeKind: "component", scopeID: f.compIDs["room-a"]},
		grantSpec{role: "deploy", scopeKind: "component", scopeID: f.compIDs["room-b"]})

	status, body := f.c.send(tok, http.MethodGet, "/components/display-1", nil)
	if status != http.StatusConflict {
		t.Fatalf("scoped GET /components/display-1 = %d, want 409\nbody: %s", status, body)
	}
	msg := problemDetail(t, body)
	if !strings.Contains(msg, "2 you can see answer to it") {
		t.Errorf("scoped 404 = %q, want a count of 2", msg)
	}
	if strings.Contains(msg, f.compIDs["room-c"]) || strings.Contains(msg, "room-c") {
		t.Errorf("scoped 404 = %q, leaks the room the caller cannot read", msg)
	}
	for _, room := range []string{"room-a", "room-b"} {
		if !strings.Contains(msg, f.compIDs[room]) {
			t.Errorf("scoped 404 = %q, want the readable candidate %s", msg, f.compIDs[room])
		}
	}

	status, body = f.c.send(tok, http.MethodGet, "/endpoints/display-1%2Fhttps", nil)
	if status != http.StatusConflict || strings.Contains(string(body), "room-c") {
		t.Errorf("scoped endpoint 404 = %d %s, want a 404 that does not name room-c", status, body)
	}
}

// TestAmbiguousNameCandidateResolvesOnTheSameRoute pins that the spelling the
// 404 offers is one the route accepts: each address resolves to its own row.
func TestAmbiguousNameCandidateResolvesOnTheSameRoute(t *testing.T) {
	t.Parallel()
	f := newAmbiguousFixture(t)

	_, body := f.c.send(f.owner, http.MethodGet, "/components/display-1", nil)
	found := candidatePattern.FindAllStringSubmatch(problemDetail(t, body), -1)
	if len(found) != 3 {
		t.Fatalf("404 listed %d candidates, want 3: %s", len(found), body)
	}
	for _, m := range found {
		addr, id := m[1], m[2]
		var got struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(f.c.do(f.owner, http.MethodGet, "/components/"+addr, nil, http.StatusOK), &got); err != nil {
			t.Fatalf("decode %s: %v", addr, err)
		}
		if got.ID != id {
			t.Errorf("GET /components/%s = %s, want the %s the 404 paired it with", addr, got.ID, id)
		}
	}
}

// TestAmbiguousNameIs409WithIDsAndUnknownIs404 pins #2334: a name several rows
// answer to is a 409 whose body lists each match with its id and carries the
// machine-readable marker the CLI keys on; a name nothing answers to stays a 404.
func TestAmbiguousNameIs409WithIDsAndUnknownIs404(t *testing.T) {
	t.Parallel()
	f := newAmbiguousFixture(t)

	status, body := f.c.send(f.owner, http.MethodGet, "/components/display-1", nil)
	if status != http.StatusConflict {
		t.Fatalf("ambiguous = %d, want 409\nbody: %s", status, body)
	}
	var p struct {
		Errors []struct {
			Location string `json:"location"`
			Value    struct {
				Count      int      `json:"count"`
				Candidates []string `json:"candidates"`
			} `json:"value"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p.Errors) != 1 || p.Errors[0].Location != api.AmbiguousRefLocation || p.Errors[0].Value.Count != 3 {
		t.Fatalf("errors = %+v, want one %s detail with count 3", p.Errors, api.AmbiguousRefLocation)
	}
	for _, room := range []string{"room-a", "room-b", "room-c"} {
		if !strings.Contains(strings.Join(p.Errors[0].Value.Candidates, " "), f.compIDs[room]) {
			t.Errorf("candidates %v lack the id of %s", p.Errors[0].Value.Candidates, room)
		}
	}

	if status, body = f.c.send(f.owner, http.MethodGet, "/components/no-such-component", nil); status != http.StatusNotFound {
		t.Errorf("unknown name = %d, want 404\nbody: %s", status, body)
	}
}
