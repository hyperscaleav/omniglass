package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/jackc/pgx/v5"
)

// ownedNameFixture is a campus > room (+ an annex beside it) with a component
// in the room, and the same secret name sealed at three tiers: the platform,
// the room, and the component. That is the cascade's ordinary shape (a nearer
// owner overrides a broader one by sharing its name), so a bare secret name is
// ambiguous by design and the owner-qualified spelling is what disambiguates.
type ownedNameFixture struct {
	gw                   storage.Gateway
	dsn                  string
	roomID               string
	platform, room, comp string // the three panel-login secrets' uuids
	solo, hidden, annex  string
	roomScope            scope.Set
}

func newOwnedNameFixture(t *testing.T) *ownedNameFixture {
	t.Helper()
	gw, dsn := secretGateway(t)
	ctx := context.Background()
	mustLoc(t, gw, "campus", "campus", nil)
	mustLoc(t, gw, "room", "room", strptr("campus"))
	mustLoc(t, gw, "annex", "room", strptr("campus"))
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec-1", LocationName: strptr("room")}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	mk := func(name, ownerKind string, owner *string, adminSensitive bool) string {
		t.Helper()
		spec := storage.SecretSpec{Name: name, SecretType: "snmp-community", OwnerKind: ownerKind, OwnerName: owner,
			Fields: map[string]string{"community": name + "-" + ownerKind}}
		if adminSensitive {
			v := true
			spec.AdminSensitive = &v
		}
		s, err := gw.CreateSecret(ctx, "", spec, all, true)
		if err != nil {
			t.Fatalf("secret %s@%s: %v", name, ownerKind, err)
		}
		return s.ID
	}
	f := &ownedNameFixture{gw: gw, dsn: dsn}
	f.platform = mk("panel-login", "platform", nil, false)
	f.room = mk("panel-login", "location", strptr("room"), false)
	f.comp = mk("panel-login", "component", strptr("codec-1"), false)
	f.solo = mk("solo", "location", strptr("room"), false)
	f.hidden = mk("hidden", "location", strptr("room"), true)
	f.annex = mk("annex-only", "location", strptr("annex"), false)
	f.roomID = mustLocID(t, gw, "room")
	f.roomScope = scope.Set{IDs: []string{f.roomID}}
	return f
}

// TestSecretAnswersToItsName pins #1133's secret key: a bare name where it is
// the only one the caller can see, `<owner>/<name>` with the owner spelled by
// dotted address, bare name, uuid or `$platform`, and the uuid as before. The
// row returned carries its uuid whichever spelling found it.
func TestSecretAnswersToItsName(t *testing.T) {
	f := newOwnedNameFixture(t)
	ctx := context.Background()
	label := "relabelled"
	for ref, want := range map[string]string{
		"solo":                                  f.solo,
		f.solo:                                  f.solo,
		"$platform/panel-login":                 f.platform,
		"campus.room/panel-login":               f.room,
		"room/panel-login":                      f.room,
		f.roomID + "/panel-login":               f.room,
		"codec-1/panel-login":                   f.comp,
		"campus.room.$comp.codec-1/panel-login": f.comp,
	} {
		got, err := f.gw.UpdateSecret(ctx, "", ref, storage.SecretPatch{Label: &label}, all, all, true, true)
		if err != nil {
			t.Fatalf("update %q: %v", ref, err)
		}
		if got.ID != want {
			t.Errorf("update %q landed on %s, want %s", ref, got.ID, want)
		}
	}

	fields, err := f.gw.RevealSecret(ctx, "", "$platform/panel-login", all, all, true)
	if err != nil || fields["community"] != "panel-login-platform" {
		t.Fatalf("reveal $platform/panel-login = %v (err %v)", fields, err)
	}
	conn, err := pgx.Connect(ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var keyed int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'secret' and verb = 'reveal' and resource_id = $1`, f.platform).Scan(&keyed); err != nil || keyed != 1 {
		t.Errorf("reveal audit rows keyed on the platform secret's uuid = %d (err %v), want 1", keyed, err)
	}
}

// TestSecretNameAmbiguityIsJudgedInScope: a name the caller can see at more than
// one owner is refused with each one's owner-qualified spelling; a caller who
// can see only one of them resolves it.
func TestSecretNameAmbiguityIsJudgedInScope(t *testing.T) {
	f := newOwnedNameFixture(t)
	ctx := context.Background()

	_, err := f.gw.RevealSecret(ctx, "", "panel-login", all, all, true)
	var unresolved *storage.ErrUnresolvedRef
	if !errors.As(err, &unresolved) || !errors.Is(err, storage.ErrSecretNotFound) {
		t.Fatalf("all-scope reveal panel-login = %v, want an ErrUnresolvedRef that is ErrSecretNotFound", err)
	}
	want := "$platform/panel-login,campus.room/panel-login,campus.room.$comp.codec-1/panel-login"
	if got := strings.Join(unresolved.Candidates, ","); got != want {
		t.Errorf("candidates = %s, want %s", got, want)
	}

	// A room-rooted read sees the room's own secret and neither the platform
	// row (all scope only) nor the component's (a location root does not reach
	// the component tier), so the bare name is unambiguous there.
	label := "x"
	got, err := f.gw.UpdateSecret(ctx, "", "panel-login", storage.SecretPatch{Label: &label}, f.roomScope, f.roomScope, false, false)
	if err != nil || got.ID != f.room {
		t.Fatalf("room-scoped update panel-login = %+v (err %v), want the room's", got, err)
	}
}

// TestSecretNameLookupDisclosesNothing: a secret the caller may not see (out of
// read scope, or admin-sensitive without the tier) refuses exactly as one that
// does not exist, in every spelling, with no candidate.
func TestSecretNameLookupDisclosesNothing(t *testing.T) {
	f := newOwnedNameFixture(t)
	ctx := context.Background()
	refusal := func(ref string, read scope.Set, canAdmin bool) string {
		t.Helper()
		_, err := f.gw.RevealSecret(ctx, "", ref, read, read, canAdmin)
		var unresolved *storage.ErrUnresolvedRef
		if !errors.As(err, &unresolved) {
			t.Fatalf("reveal %q = %v, want ErrUnresolvedRef", ref, err)
		}
		if len(unresolved.Candidates) != 0 {
			t.Errorf("reveal %q named candidates %v", ref, unresolved.Candidates)
		}
		return strings.ReplaceAll(unresolved.Error(), ref, "REF")
	}
	absent := refusal("no-such-secret", all, true)
	if got := refusal("hidden", all, false); got != absent {
		t.Errorf("admin-sensitive without the tier refuses %q, absent refuses %q", got, absent)
	}
	if got := refusal("annex-only", f.roomScope, false); got != absent {
		t.Errorf("out-of-scope refuses %q, absent refuses %q", got, absent)
	}
	if got := refusal("$platform/panel-login", f.roomScope, false); got != absent {
		t.Errorf("a platform secret to a scoped caller refuses %q, absent refuses %q", got, absent)
	}
	if got := refusal("annex/annex-only", f.roomScope, false); got != absent {
		t.Errorf("an owner-qualified out-of-scope secret refuses %q, absent refuses %q", got, absent)
	}
}

// TestSecretWritesByNameKeepTheScopeSplit: a name changes how the secret is
// found, never what may be done to it.
func TestSecretWritesByNameKeepTheScopeSplit(t *testing.T) {
	f := newOwnedNameFixture(t)
	ctx := context.Background()
	label := "x"
	if _, err := f.gw.UpdateSecret(ctx, "", "solo", storage.SecretPatch{Label: &label}, f.roomScope, scope.Set{}, false, false); !errors.Is(err, storage.ErrSecretForbidden) {
		t.Errorf("update readable-not-actionable by name = %v, want ErrSecretForbidden", err)
	}
	if _, err := f.gw.UpdateSecret(ctx, "", "$platform/panel-login", storage.SecretPatch{Label: &label}, all, all, true, false); !errors.Is(err, storage.ErrSecretForbidden) {
		t.Errorf("update a platform secret by name without the platform permission = %v, want ErrSecretForbidden", err)
	}
	if err := f.gw.DeleteSecret(ctx, "", "solo", f.roomScope, f.roomScope, false, false); err != nil {
		t.Fatalf("delete by name: %v", err)
	}
	if _, err := f.gw.RevealSecret(ctx, "", f.solo, all, all, true); !errors.Is(err, storage.ErrSecretNotFound) {
		t.Errorf("reveal the deleted secret by uuid = %v, want ErrSecretNotFound", err)
	}
}

// TestVariableAnswersToItsName pins the same key on variables, the other
// owner-arc table: a bare name, `<owner>/<name>`, and the uuid; a shared name
// refused with its owner-qualified spellings; scope narrowing first.
func TestVariableAnswersToItsName(t *testing.T) {
	f := newOwnedNameFixture(t)
	ctx := context.Background()
	mk := func(ownerKind string, owner *string) string {
		t.Helper()
		v, err := f.gw.CreateVariable(ctx, "", storage.VariableSpec{
			Name: "poll-interval", ValueType: "int", OwnerKind: ownerKind, OwnerName: owner, Value: json.RawMessage(`30`),
		}, all)
		if err != nil {
			t.Fatalf("variable@%s: %v", ownerKind, err)
		}
		return v.ID
	}
	plat, room := mk("platform", nil), mk("location", strptr("room"))

	for ref, want := range map[string]string{
		"$platform/poll-interval":   plat,
		"room/poll-interval":        room,
		"campus.room/poll-interval": room,
		room:                        room,
	} {
		got, err := f.gw.UpdateVariable(ctx, "", ref, storage.VariablePatch{Value: json.RawMessage(`60`)}, all, all, true)
		if err != nil || got.ID != want {
			t.Fatalf("update %q = %+v (err %v), want %s", ref, got, err, want)
		}
	}
	_, err := f.gw.UpdateVariable(ctx, "", "poll-interval", storage.VariablePatch{Value: json.RawMessage(`60`)}, all, all, true)
	var unresolved *storage.ErrUnresolvedRef
	if !errors.As(err, &unresolved) || !errors.Is(err, storage.ErrVariableNotFound) ||
		strings.Join(unresolved.Candidates, ",") != "$platform/poll-interval,campus.room/poll-interval" {
		t.Fatalf("all-scope update poll-interval = %v, want ambiguous with both spellings", err)
	}
	got, err := f.gw.UpdateVariable(ctx, "", "poll-interval", storage.VariablePatch{Value: json.RawMessage(`90`)}, f.roomScope, f.roomScope, false)
	if err != nil || got.ID != room {
		t.Fatalf("room-scoped update poll-interval = %+v (err %v), want the room's", got, err)
	}
	if err := f.gw.DeleteVariable(ctx, "", "$platform/poll-interval", all, all, false); !errors.Is(err, storage.ErrVariableForbidden) {
		t.Errorf("delete a platform variable by name without the platform permission = %v, want ErrVariableForbidden", err)
	}
	if err := f.gw.DeleteVariable(ctx, "", "room/poll-interval", f.roomScope, f.roomScope, false); err != nil {
		t.Fatalf("delete by name: %v", err)
	}
}
