package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The acknowledge route end to end (#734), driven as a caller drives it.
//
// Every principal here is NARROW. Three scope defects in this arc reached review
// because the fixture drove an owner holding `all`, under which every scope
// question answers yes; an owner runs beside them only as the control that proves
// a refusal is a scope boundary and not a broken route.
//
// The two layers are separate, and the fixture separates them: `alarm:acknowledge`
// is the capability (a fast reject in middleware), and the component tier the
// grant is scoped at is the reach. A principal can hold the capability everywhere
// and the reach nowhere, or read a component estate-wide and acknowledge on none
// of it, and both must refuse.

type ackFixture struct {
	c     *apiClient
	owner string
	// responder holds operator (which carries alarm:acknowledge) scoped to ONE
	// component, beside the all-scoped viewer floor a real principal carries. The
	// floor is what makes the test meaningful: it can READ every component in the
	// estate and must be able to acknowledge on only one of them.
	responder string
	// watcher holds the viewer floor alone: it sees both alarms and holds no
	// acknowledgement at all, so it is refused before scope is ever consulted.
	watcher string
	// mine and theirs are two components, one inside the responder's grant and one
	// outside it, each carrying a raised alarm.
	mine, theirs             string
	mineAlarm, theirsAlarm   string
	mineCompID, theirsCompID string
}

func newAckFixture(t *testing.T) *ackFixture {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	owner := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	f := &ackFixture{c: c, owner: owner, mine: "codec-mine", theirs: "codec-theirs"}

	for _, name := range []string{f.mine, f.theirs} {
		c.do(owner, http.MethodPost, "/components", map[string]any{
			"name": name, "product": "generic-device",
		}, http.StatusCreated)
	}
	f.mineCompID = entityID(t, c, owner, "/components", f.mine)
	f.theirsCompID = entityID(t, c, owner, "/components", f.theirs)
	f.mineAlarm = raiseFor(t, c, owner, f.mine, "critical", "no-signal", "no signal")
	f.theirsAlarm = raiseFor(t, c, owner, f.theirs, "critical", "no-signal", "no signal")

	floor := grant{role: "viewer", scopeKind: "all"}
	f.responder = principalWithGrants(t, ctx, dsn, "responder", []grant{
		{role: "operator", scopeKind: "component", scopeID: f.mineCompID}, floor,
	})
	f.watcher = principalWithGrants(t, ctx, dsn, "watcher", []grant{floor})
	return f
}

func raiseFor(t *testing.T, c *apiClient, tok, component, severity, dedupKey, message string) string {
	t.Helper()
	var body struct {
		ID string `json:"id"`
	}
	raw := c.do(tok, http.MethodPost, "/components/"+component+"/alarms", map[string]any{
		"severity": severity, "message": message, "dedup_key": dedupKey,
	}, http.StatusCreated)
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode raise: %v", err)
	}
	return body.ID
}

func readAlarm(t *testing.T, c *apiClient, tok, component, id string) alarmView {
	t.Helper()
	var env struct {
		Alarms []alarmView `json:"alarms"`
	}
	raw := c.do(tok, http.MethodGet, "/components/"+component+"/alarms?include_cleared=true", nil, http.StatusOK)
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode alarms: %v", err)
	}
	for _, a := range env.Alarms {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("alarm %s is not in %+v", id, env.Alarms)
	return alarmView{}
}

type alarmView struct {
	ID             string  `json:"id"`
	Active         bool    `json:"active"`
	ClearedAt      *string `json:"cleared_at"`
	Acknowledged   bool    `json:"acknowledged"`
	AcknowledgedAt *string `json:"acknowledged_at"`
	AcknowledgedBy string  `json:"acknowledged_by"`
}

func ackPath(component, id string) string {
	return "/components/" + component + "/alarms/" + id + ":acknowledge"
}

// TestAnOperatorAcknowledgesInScope is the happy path, driven by the narrow
// principal rather than by the owner.
func TestAnOperatorAcknowledgesInScope(t *testing.T) {
	f := newAckFixture(t)

	var got alarmView
	raw := f.c.do(f.responder, http.MethodPost, ackPath(f.mine, f.mineAlarm), nil, http.StatusOK)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode acknowledge: %v", err)
	}
	if !got.Acknowledged || got.AcknowledgedAt == nil {
		t.Fatalf("acknowledge returned %+v, want an acknowledgement", got)
	}
	if got.AcknowledgedBy != "responder" {
		t.Errorf("acknowledged by %q, want the calling principal", got.AcknowledgedBy)
	}
	if !got.Active || got.ClearedAt != nil {
		t.Error("acknowledging cleared the alarm: the raised state belongs to the condition, not to the person")
	}

	// And it is durable, read back through the ordinary list.
	back := readAlarm(t, f.c, f.owner, f.mine, f.mineAlarm)
	if !back.Acknowledged || !back.Active {
		t.Errorf("read back %+v, want an acknowledged alarm that is still raised", back)
	}
}

// TestAcknowledgeRefusesWithoutTheCapability drives the fast-reject layer: the
// watcher can READ this alarm perfectly well and holds no acknowledgement, so it
// is a 403 rather than a non-disclosing 404. It is not an owner-shaped principal
// and the refusal is not a broken route, which the owner control below proves.
func TestAcknowledgeRefusesWithoutTheCapability(t *testing.T) {
	f := newAckFixture(t)

	// The watcher really can see it: the refusal is about the verb, not the row.
	if got := readAlarm(t, f.c, f.watcher, f.mine, f.mineAlarm); got.ID != f.mineAlarm {
		t.Fatalf("the watcher cannot read the alarm it is supposed to be refused on: %+v", got)
	}
	f.c.do(f.watcher, http.MethodPost, ackPath(f.mine, f.mineAlarm), nil, http.StatusForbidden)

	// Control: the same body, from a principal that holds the capability, lands.
	f.c.do(f.owner, http.MethodPost, ackPath(f.mine, f.mineAlarm), nil, http.StatusOK)

	// And the refusal really refused: the alarm the watcher was turned away from
	// carries the OWNER's acknowledgement, never the watcher's.
	if got := readAlarm(t, f.c, f.owner, f.mine, f.mineAlarm); got.AcknowledgedBy != "owner-all" {
		t.Errorf("alarm acknowledged by %q, want owner-all: the refused call landed anyway", got.AcknowledgedBy)
	}
}

// TestAcknowledgeRefusesOutsideTheScope is the ABAC layer, and the one a fixture
// driven by an owner cannot see. The responder HOLDS alarm:acknowledge (so the
// middleware admits it) and can read every component in the estate through its
// viewer floor, and its acknowledgement reaches exactly one component.
//
// The far component is therefore READABLE and not actionable, and #736 changed
// what that is owed: the refusal was a 404 (this route resolved its component
// with the acknowledgement scope alone, so a component outside it came back
// absent) and is now the truthful 403. Nothing about the reach changed, only
// what the platform says about it. Telling this caller "not found" about a row
// it is looking straight at was a lie it could catch, and #728 filed it rather
// than fixing one route out of step with its siblings.
func TestAcknowledgeRefusesOutsideTheScope(t *testing.T) {
	f := newAckFixture(t)

	// It really can read the far component and its alarm. This is the condition
	// that makes the 403 below safe: the refusal names a row this caller could
	// have read anyway, so it discloses nothing a GET would not.
	if got := readAlarm(t, f.c, f.responder, f.theirs, f.theirsAlarm); got.ID != f.theirsAlarm {
		t.Fatalf("the responder cannot read the far alarm, so this test is not about scope: %+v", got)
	}
	f.c.do(f.responder, http.MethodPost, ackPath(f.theirs, f.theirsAlarm), nil, http.StatusForbidden)

	// The near one, same principal, same verb: allowed. So the refusal above is
	// the scope boundary and not the route.
	f.c.do(f.responder, http.MethodPost, ackPath(f.mine, f.mineAlarm), nil, http.StatusOK)

	if got := readAlarm(t, f.c, f.owner, f.theirs, f.theirsAlarm); got.Acknowledged {
		t.Error("the out-of-scope acknowledgement landed anyway")
	}
}

// TestAcknowledgeIsIdempotentOverTheRoute pins the ruling at the surface an
// operator drives: a second acknowledgement is 200 and keeps the first person,
// not a 409 an operator has to interpret after a double click.
func TestAcknowledgeIsIdempotentOverTheRoute(t *testing.T) {
	f := newAckFixture(t)

	f.c.do(f.responder, http.MethodPost, ackPath(f.mine, f.mineAlarm), nil, http.StatusOK)
	var second alarmView
	raw := f.c.do(f.owner, http.MethodPost, ackPath(f.mine, f.mineAlarm), nil, http.StatusOK)
	if err := json.Unmarshal(raw, &second); err != nil {
		t.Fatalf("decode second acknowledge: %v", err)
	}
	if second.AcknowledgedBy != "responder" {
		t.Errorf("the second acknowledgement reassigned the alarm to %q; the first person to look is the one recorded", second.AcknowledgedBy)
	}
}

// TestAcknowledgeMissesAre404 keeps a typo off the 500 path and keeps an
// out-of-scope component non-disclosing.
func TestAcknowledgeMissesAre404(t *testing.T) {
	f := newAckFixture(t)

	f.c.do(f.owner, http.MethodPost, ackPath(f.mine, "00000000-0000-0000-0000-000000000000"), nil, http.StatusNotFound)
	f.c.do(f.owner, http.MethodPost, ackPath("codec-404", f.mineAlarm), nil, http.StatusNotFound)
	// An alarm id that belongs to the OTHER component is a miss on this one, even
	// for an owner: the address names a component and an alarm together.
	f.c.do(f.owner, http.MethodPost, ackPath(f.mine, f.theirsAlarm), nil, http.StatusNotFound)
}

// TestTheReadSideExpressesRaisedAndUnacknowledged drives the queue an operator
// actually works, over the route.
func TestTheReadSideExpressesRaisedAndUnacknowledged(t *testing.T) {
	f := newAckFixture(t)

	// A second condition on the same component, so the filter has something to
	// exclude rather than an empty set to trivially satisfy.
	fan := raiseFor(t, f.c, f.owner, f.mine, "warning", "fan.degraded", "fan degraded")
	f.c.do(f.responder, http.MethodPost, ackPath(f.mine, fan), nil, http.StatusOK)

	var env struct {
		Alarms []alarmView `json:"alarms"`
	}
	raw := f.c.do(f.responder, http.MethodGet, "/components/"+f.mine+"/alarms?unacknowledged=true", nil, http.StatusOK)
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	if len(env.Alarms) != 1 || env.Alarms[0].ID != f.mineAlarm {
		t.Fatalf("the raised-and-unacknowledged queue is %+v, want only %s", env.Alarms, f.mineAlarm)
	}
}
