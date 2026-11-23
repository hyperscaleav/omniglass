package storage_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The fleet's alarm list (#2025) is what is wrong across every component the
// caller may read, in one query: worst first, the Storage Gateway injecting the
// caller's component scope, so an agent finds what is red without walking every
// component and a scoped caller never learns an out-of-scope alarm exists.
func TestListFleetAlarmsIsTheActiveSetInScopeWorstFirst(t *testing.T) {
	t.Parallel()
	gw, dsn := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	ids := map[string]string{}
	for _, c := range []string{"fleet-dsp", "fleet-mic", "fleet-cam"} {
		comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: c}, all, all, all, all)
		if err != nil {
			t.Fatalf("component %s: %v", c, err)
		}
		ids[c] = comp.ID
	}
	raise := func(comp, severity, msg string) *storage.Alarm {
		t.Helper()
		a, err := gw.RaiseAlarm(ctx, "", comp, storage.AlarmSpec{Severity: severity, Message: msg})
		if err != nil {
			t.Fatalf("raise %s: %v", msg, err)
		}
		return a
	}
	warn := raise("fleet-dsp", "warning", "fan slow")
	worst := raise("fleet-mic", "disaster", "no audio")
	high := raise("fleet-cam", "high", "no video")
	cleared := raise("fleet-cam", "high", "flapped")
	if err := gw.ClearAlarm(ctx, "", "fleet-cam", cleared.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := gw.AcknowledgeAlarm(ctx, ackActor(t, dsn, "fleet-ops"), "fleet-cam", high.ID); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}

	names := func(as []storage.Alarm) []string {
		out := make([]string, 0, len(as))
		for _, a := range as {
			out = append(out, a.Message)
		}
		return out
	}
	got, _, err := gw.ListFleetAlarms(ctx, all, all, storage.AlarmFilter{}, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 || got[0].ID != worst.ID || got[1].ID != high.ID || got[2].ID != warn.ID {
		t.Fatalf("fleet alarms = %v, want [no audio, no video, fan slow]: active only, worst severity first", names(got))
	}
	if got[0].ComponentName != "fleet-mic" {
		t.Errorf("the worst alarm names component %q, want fleet-mic", got[0].ComponentName)
	}

	if got, _, err := gw.ListFleetAlarms(ctx, all, all, storage.AlarmFilter{Unacknowledged: true}, 100); err != nil || len(got) != 2 {
		t.Fatalf("unacknowledged = %v (%v), want the two nobody has looked at", names(got), err)
	}
	if got, _, err := gw.ListFleetAlarms(ctx, all, all, storage.AlarmFilter{}, 1); err != nil || len(got) != 1 || got[0].ID != worst.ID {
		t.Fatalf("capped at 1 = %v (%v), want the worst alone", names(got), err)
	}

	scoped := scope.Set{IDs: []string{ids["fleet-dsp"]}}
	if got, _, err := gw.ListFleetAlarms(ctx, scoped, all, storage.AlarmFilter{}, 100); err != nil || len(got) != 1 || got[0].ID != warn.ID {
		t.Fatalf("scoped to fleet-dsp = %v (%v), want its one alarm and nothing else", names(got), err)
	}
	if got, _, err := gw.ListFleetAlarms(ctx, scope.Set{}, all, storage.AlarmFilter{}, 100); err != nil || len(got) != 0 {
		t.Fatalf("empty scope = %v (%v), want nothing", names(got), err)
	}
	if got, _, err := gw.ListFleetAlarms(ctx, scope.Set{IDs: []string{"not-a-uuid"}}, all, storage.AlarmFilter{}, 100); err != nil || len(got) != 0 {
		t.Fatalf("malformed scope root = %v (%v), want nothing", names(got), err)
	}
}

// The alarm queue (#2333) is the fleet list an operator triages from: each row
// names where the component sits and which systems it belongs to, the list
// narrows by severity, location subtree, acknowledgement and snooze, and a long
// queue pages by a cursor in the same worst-first order rather than truncating.
func TestListFleetAlarmsFiltersPagesAndNamesLocationAndSystems(t *testing.T) {
	t.Parallel()
	gw, dsn := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	for _, l := range []storage.LocationSpec{
		{Name: "q-hq", LocationType: "campus"},
		{Name: "q-r1", LocationType: "room", ParentName: strptr("q-hq")},
		{Name: "q-other", LocationType: "campus"},
	} {
		if _, err := gw.CreateLocation(ctx, "", l, all); err != nil {
			t.Fatalf("location %s: %v", l.Name, err)
		}
	}
	mk := func(name, loc string) {
		t.Helper()
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name, LocationName: strptr(loc)}, all, all, all, all); err != nil {
			t.Fatalf("component %s: %v", name, err)
		}
	}
	mk("q-a", "q-r1")
	mk("q-b", "q-other")
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "q-sys"}, all, all); err != nil {
		t.Fatalf("system: %v", err)
	}
	if err := gw.AddMember(ctx, "", "q-sys", "q-a", all, all, all, all); err != nil {
		t.Fatalf("member: %v", err)
	}
	raise := func(comp, sev, key string) *storage.Alarm {
		t.Helper()
		a, err := gw.RaiseAlarm(ctx, "", comp, storage.AlarmSpec{Severity: sev, Message: key, DedupKey: key})
		if err != nil {
			t.Fatalf("raise %s: %v", key, err)
		}
		return a
	}
	crit := raise("q-a", "disaster", "crit")
	warn := raise("q-a", "warning", "warn")
	far := raise("q-b", "high", "far")
	if _, err := gw.AcknowledgeAlarm(ctx, ackActor(t, dsn, "q-ops"), "q-a", warn.ID); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if _, err := gw.SnoozeAlarm(ctx, ackActor(t, dsn, "q-ops2"), "q-b", far.ID, time.Hour); err != nil {
		t.Fatalf("snooze: %v", err)
	}

	ids := func(as []storage.Alarm) []string {
		out := []string{}
		for _, a := range as {
			out = append(out, a.Message)
		}
		return out
	}
	list := func(f storage.AlarmFilter) []string {
		t.Helper()
		got, _, err := gw.ListFleetAlarms(ctx, all, all, f, 100)
		if err != nil {
			t.Fatalf("list %+v: %v", f, err)
		}
		return ids(got)
	}
	eq := func(got []string, want ...string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	tr, fa := true, false

	rows, _, _ := gw.ListFleetAlarms(ctx, all, all, storage.AlarmFilter{}, 100)
	if len(rows) != 3 || rows[0].ID != crit.ID {
		t.Fatalf("rows = %v", ids(rows))
	}
	if rows[0].LocationName != "q-r1" || strings.Join(rows[0].LocationPath, ".") != "q-hq.q-r1" || strings.Join(rows[0].Systems, ",") != "q-sys" {
		t.Errorf("crit row location %q path %v systems %v, want q-r1 / q-hq.q-r1 / [q-sys]", rows[0].LocationName, rows[0].LocationPath, rows[0].Systems)
	}
	if rows[1].LocationName != "q-other" || len(rows[1].Systems) != 0 {
		t.Errorf("far row location %q systems %v, want q-other and no systems", rows[1].LocationName, rows[1].Systems)
	}

	eq(list(storage.AlarmFilter{Severities: []string{"disaster", "warning"}}), "crit", "warn")
	eq(list(storage.AlarmFilter{Location: "q-hq"}), "crit", "warn")
	eq(list(storage.AlarmFilter{Location: "q-other"}), "far")
	eq(list(storage.AlarmFilter{Acknowledged: &tr}), "warn")
	eq(list(storage.AlarmFilter{Acknowledged: &fa}), "crit", "far")
	eq(list(storage.AlarmFilter{Snoozed: &tr}), "far")
	eq(list(storage.AlarmFilter{Snoozed: &fa}), "crit", "warn")

	// Paging: one at a time walks the whole queue in order, then ends.
	var walked []string
	token := ""
	for range 5 {
		page, next, err := gw.ListFleetAlarms(ctx, all, all, storage.AlarmFilter{PageToken: token}, 1)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		walked = append(walked, ids(page)...)
		if next == "" {
			break
		}
		token = next
	}
	eq(walked, "crit", "far", "warn")
	if _, _, err := gw.ListFleetAlarms(ctx, all, all, storage.AlarmFilter{PageToken: "garbage"}, 1); !errors.Is(err, storage.ErrPageToken) {
		t.Fatalf("bad token err = %v, want ErrPageToken", err)
	}
}

// The fleet list reads the cleared alarms too (#2426): state picks the active
// set (the default), the cleared ones, or all of them, and since/until bound the
// overlap of each alarm's raised-to-cleared interval with the range, so an alarm
// raised before the range and still open in it is kept, the same semantics the
// component and system alarm reads take.
func TestListFleetAlarmsReadsClearedAlarmsOverARange(t *testing.T) {
	t.Parallel()
	gw, dsn := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "h-disp"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	raise := func(key string) string {
		t.Helper()
		a, err := gw.RaiseAlarm(ctx, "", "h-disp", storage.AlarmSpec{Severity: "high", Message: key, DedupKey: key})
		if err != nil {
			t.Fatalf("raise %s: %v", key, err)
		}
		return a.ID
	}
	before, inside, across, after := raise("before"), raise("inside"), raise("across"), raise("after")
	for _, id := range []string{before, inside} {
		if err := gw.ClearAlarm(ctx, "", "h-disp", id); err != nil {
			t.Fatalf("clear: %v", err)
		}
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	place := func(id, raised, cleared string) {
		t.Helper()
		if _, err := conn.Exec(ctx, `update alarm set raised_at = now() - $2::interval,
			cleared_at = case when $3 = '' then null else now() - $3::interval end where id = $1`, id, raised, cleared); err != nil {
			t.Fatalf("place: %v", err)
		}
	}
	// The range is 10h..2h ago.
	place(before, "20 hours", "15 hours")
	place(inside, "6 hours", "5 hours")
	place(across, "12 hours", "")
	place(after, "1 hour", "")

	list := func(f storage.AlarmFilter) string {
		t.Helper()
		got, _, err := gw.ListFleetAlarms(ctx, all, all, f, 100)
		if err != nil {
			t.Fatalf("list %+v: %v", f, err)
		}
		names := []string{}
		for _, a := range got {
			names = append(names, a.Message)
		}
		sort.Strings(names)
		return strings.Join(names, ",")
	}
	since, until := time.Now().Add(-10*time.Hour), time.Now().Add(-2*time.Hour)
	for _, c := range []struct {
		name string
		f    storage.AlarmFilter
		want string
	}{
		{"default is active", storage.AlarmFilter{}, "across,after"},
		{"cleared", storage.AlarmFilter{State: storage.AlarmStateCleared}, "before,inside"},
		{"all", storage.AlarmFilter{State: storage.AlarmStateAll}, "across,after,before,inside"},
		{"all over the range", storage.AlarmFilter{State: storage.AlarmStateAll, Since: since, Until: until}, "across,inside"},
		{"cleared over the range", storage.AlarmFilter{State: storage.AlarmStateCleared, Since: since, Until: until}, "inside"},
		{"active over the range", storage.AlarmFilter{Since: since, Until: until}, "across"},
		{"since only", storage.AlarmFilter{State: storage.AlarmStateAll, Since: since}, "across,after,inside"},
		{"until only", storage.AlarmFilter{State: storage.AlarmStateAll, Until: until}, "across,before,inside"},
	} {
		if got := list(c.f); got != c.want {
			t.Errorf("%s = %s, want %s", c.name, got, c.want)
		}
	}
	// A cleared row carries when it cleared, for the duration the console shows.
	got, _, _ := gw.ListFleetAlarms(ctx, all, all, storage.AlarmFilter{State: storage.AlarmStateCleared}, 100)
	for _, a := range got {
		if a.ClearedAt == nil {
			t.Errorf("cleared alarm %s has no cleared_at", a.Message)
		}
	}
}

// A component placed only through its system has no location of its own: the
// alarm row names the location of the system it sits in instead of a blank
// (#2426).
func TestFleetAlarmLocationFallsBackToTheSystemsLocation(t *testing.T) {
	t.Parallel()
	gw, _ := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	for _, l := range []storage.LocationSpec{
		{Name: "fb-hq", LocationType: "campus"},
		{Name: "fb-r1", LocationType: "room", ParentName: strptr("fb-hq")},
	} {
		if _, err := gw.CreateLocation(ctx, "", l, all); err != nil {
			t.Fatalf("location %s: %v", l.Name, err)
		}
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "fb-comp"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "fb-sys", LocationName: strptr("fb-r1")}, all, all); err != nil {
		t.Fatalf("system: %v", err)
	}
	if err := gw.AddMember(ctx, "", "fb-sys", "fb-comp", all, all, all, all); err != nil {
		t.Fatalf("member: %v", err)
	}
	if _, err := gw.RaiseAlarm(ctx, "", "fb-comp", storage.AlarmSpec{Severity: "high", Message: "x", DedupKey: "x"}); err != nil {
		t.Fatalf("raise: %v", err)
	}
	rows, _, err := gw.ListFleetAlarms(ctx, all, all, storage.AlarmFilter{}, 100)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list = %d rows (%v)", len(rows), err)
	}
	if rows[0].LocationName != "fb-r1" || strings.Join(rows[0].LocationPath, ".") != "fb-hq.fb-r1" {
		t.Errorf("location %q path %v, want fb-r1 / fb-hq.fb-r1 through the system", rows[0].LocationName, rows[0].LocationPath)
	}
	if loc, err := gw.GetLocation(ctx, "fb-r1", all); err != nil || rows[0].LocationID != loc.ID {
		t.Errorf("location id %q, want fb-r1's uuid (%v)", rows[0].LocationID, err)
	}
}

// The system-location fallback only ever reads systems the caller may read: a
// component also in a hidden system that shares a visible system's name is not
// placed by the hidden one (#2426, agent-review).
func TestFleetAlarmLocationFallbackIgnoresHiddenSameNamedSystems(t *testing.T) {
	t.Parallel()
	gw, dsn := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hid-vault", LocationType: "campus"}, all); err != nil {
		t.Fatalf("location: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "hid-comp"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	// "twin" is visible and has no location; the other "twin" is hidden and sits in hid-vault.
	visible, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "twin"}, all, all)
	if err != nil {
		t.Fatalf("visible system: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "twin", LocationName: strptr("hid-vault")}, all, all); err != nil {
		t.Fatalf("hidden system: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	// Both twins hold the component; AddMember takes names, which are ambiguous here.
	if _, err := conn.Exec(ctx, `insert into system_member (system_id, component_id)
		select s.id, c.id from system s, component c where s.name = 'twin' and c.name = 'hid-comp'`); err != nil {
		t.Fatalf("members: %v", err)
	}
	if _, err := gw.RaiseAlarm(ctx, "", "hid-comp", storage.AlarmSpec{Severity: "high", Message: "x", DedupKey: "x"}); err != nil {
		t.Fatalf("raise: %v", err)
	}
	rows, _, err := gw.ListFleetAlarms(ctx, all, scope.Set{IDs: []string{visible.ID}}, storage.AlarmFilter{}, 100)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list = %d rows (%v)", len(rows), err)
	}
	if len(rows[0].SystemIDs) != 1 || rows[0].SystemIDs[0] != visible.ID {
		t.Fatalf("system ids %v, want only the visible twin", rows[0].SystemIDs)
	}
	if rows[0].LocationName != "" || rows[0].LocationID != "" {
		t.Errorf("location %q (%s) leaked from the hidden twin in hid-vault", rows[0].LocationName, rows[0].LocationID)
	}
}

// Two visible systems sharing a name come back in one fixed order, by name and
// then id, in both parallel arrays, so system_ids[i] always names systems[i]
// (#2426, agent-review).
func TestFleetAlarmSystemsAndIDsStayAlignedForSameNamedSystems(t *testing.T) {
	t.Parallel()
	gw, dsn := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	for _, l := range []string{"al-a", "al-b", "al-c"} {
		if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: l, LocationType: "campus"}, all); err != nil {
			t.Fatalf("location: %v", err)
		}
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "al-comp"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	for _, l := range []string{"al-a", "al-b", "al-c"} {
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "twin", LocationName: strptr(l)}, all, all); err != nil {
			t.Fatalf("system: %v", err)
		}
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into system_member (system_id, component_id)
		select s.id, c.id from system s, component c where s.name = 'twin' and c.name = 'al-comp'`); err != nil {
		t.Fatalf("members: %v", err)
	}
	if _, err := gw.RaiseAlarm(ctx, "", "al-comp", storage.AlarmSpec{Severity: "high", Message: "x", DedupKey: "x"}); err != nil {
		t.Fatalf("raise: %v", err)
	}
	rows, _, err := gw.ListFleetAlarms(ctx, all, all, storage.AlarmFilter{}, 100)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list = %d rows (%v)", len(rows), err)
	}
	ids := rows[0].SystemIDs
	if len(ids) != 3 || len(rows[0].Systems) != 3 || !sort.StringsAreSorted(ids) {
		t.Errorf("system ids %v for systems %v, want three ids in ascending order under the tied name", ids, rows[0].Systems)
	}
}
