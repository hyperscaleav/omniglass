package template_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/template"
)

// fixtureCatalog is a small catalog naming only rows the seed plants, so the
// fork tests own the shape they assert on.
const fixtureCatalog = `templates:
  - kind: standard_template
    name: base-room
    label: Base Room
    standard:
      choices:
        - name: conferencing
          label: Conferencing
          alternates:
            - { name: all-in-one, label: All-in-one }
            - { name: kit, label: Kit }
      roles:
        - { name: main-display, label: Main Display, quorum: 2, capacity: 3, impact: outage, accepted_types: [display] }
        - { name: bar, label: Bar, accepted_types: [video-bar], choice: conferencing, alternate: all-in-one }
        - { name: codec, label: Codec, accepted_types: [codec], choice: conferencing, alternate: kit }
      metrics:
        - { name: room-temperature, default: 21.5 }
      properties:
        - { name: firmware-version, required: true }
      map:
        aspect: 1.5
        positions:
          - { role: main-display, position: 1, x: 0.5, y: 0.1 }
  - kind: standard_template
    name: base-room-wide
    label: Base Room, wide
    standard:
      parent: base-room
      roles:
        - { name: main-display, label: Main Display, quorum: 2, accepted_types: [display] }
`

func fixture(t *testing.T) *template.Catalog {
	t.Helper()
	cat, err := template.Load(map[string][]byte{"f.yaml": []byte(fixtureCatalog)})
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return cat
}

func auditRows(t *testing.T, gw storage.Gateway, verb, resourceID string) []storage.AuditEntry {
	t.Helper()
	rows, err := gw.ListAuditLog(context.Background(), storage.AuditFilter{Limit: 500})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var out []storage.AuditEntry
	for _, r := range rows {
		if r.Verb == verb && r.ResourceID == resourceID {
			out = append(out, r)
		}
	}
	return out
}

func TestCreateFromTemplateWritesTheStandardWhole(t *testing.T) {
	gw := seeded(t)
	ctx := context.Background()
	cat := fixture(t)

	st, err := template.CreateStandardFromTemplate(ctx, gw, cat, "", "base-room", template.StandardOptions{})
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if st.Name != "base-room" || st.Label != "Base Room" || st.Official || st.ParentStandardID != nil {
		t.Fatalf("forked = %+v, want an operator-owned base-room with no parent", st)
	}
	if len(st.Map) == 0 {
		t.Error("the fork dropped the template's map")
	}

	roles, err := gw.ListSystemRoles(ctx, "standard", "base-room")
	if err != nil {
		t.Fatalf("roles: %v", err)
	}
	byName := map[string]storage.SystemRole{}
	for _, r := range roles {
		byName[r.Name] = r
	}
	if len(byName) != 3 {
		t.Fatalf("forked %d roles, want the template's 3: %+v", len(byName), roles)
	}
	disp := byName["main-display"]
	if disp.Quorum != 2 || disp.Capacity == nil || *disp.Capacity != 3 || disp.Impact != "outage" {
		t.Errorf("main-display = quorum %d capacity %v impact %s, want 2, 3, outage", disp.Quorum, disp.Capacity, disp.Impact)
	}
	if got := disp.AcceptedTypes; len(got) != 1 || got[0] != "display" {
		t.Errorf("main-display accepts %v, want [display]", got)
	}
	if byName["bar"].Alternate != "conferencing/all-in-one" || byName["codec"].Alternate != "conferencing/kit" {
		t.Errorf("choice roles joined %q and %q, want their alternates", byName["bar"].Alternate, byName["codec"].Alternate)
	}

	metrics, err := gw.ListStandardMetrics(ctx, "base-room")
	if err != nil || len(metrics) != 1 || metrics[0].MetricTypeName != "room-temperature" || string(metrics[0].DefaultValue) != "21.5" {
		t.Errorf("metric contract = %+v (%v), want room-temperature defaulting 21.5", metrics, err)
	}
	props, err := gw.ListStandardProperties(ctx, "base-room")
	if err != nil || len(props) != 1 || props[0].PropertyTypeName != "firmware-version" || !props[0].Required {
		t.Errorf("property contract = %+v (%v), want firmware-version required", props, err)
	}

	// One audit row, a fork naming the template.
	rows := auditRows(t, gw, "create", st.ID)
	if len(rows) != 1 {
		t.Fatalf("fork audit rows = %d, want 1", len(rows))
	}
	if !jsonHas(rows[0].New, "from_template", "base-room") {
		t.Errorf("the fork's audit detail does not name the template: %s", rows[0].New)
	}

	// Again under the same name is taken; under a second name it is a
	// standard of its own.
	if _, err := template.CreateStandardFromTemplate(ctx, gw, cat, "", "base-room", template.StandardOptions{}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("fork again under the same name: %v, want ErrTypeExists", err)
	}
	second, err := template.CreateStandardFromTemplate(ctx, gw, cat, "", "base-room", template.StandardOptions{Name: "hq-room", Label: "HQ Room"})
	if err != nil {
		t.Fatalf("fork under a second name: %v", err)
	}
	if second.ID == st.ID || second.Label != "HQ Room" {
		t.Fatalf("second fork = %+v, want an independent row", second)
	}
	if err := gw.DeleteSystemRole(ctx, "", "standard", "hq-room", "codec"); err != nil {
		t.Fatalf("edit the second fork: %v", err)
	}
	if again, _ := gw.ListSystemRoles(ctx, "standard", "base-room"); len(again) != 3 {
		t.Errorf("editing one fork changed the other: base-room has %d roles", len(again))
	}
}

func TestAVariantIsCreatedOnlyOnceItsParentIsHeld(t *testing.T) {
	gw := seeded(t)
	ctx := context.Background()
	cat := fixture(t)

	if _, err := template.CreateStandardFromTemplate(ctx, gw, cat, "", "base-room-wide", template.StandardOptions{}); !errors.Is(err, storage.ErrTemplateParentMissing) {
		t.Fatalf("fork a variant first: %v, want ErrTemplateParentMissing", err)
	}
	if _, err := gw.GetStandard(ctx, "base-room-wide"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("a refused variant fork landed: %v", err)
	}
	if _, err := template.CreateStandardFromTemplate(ctx, gw, cat, "", "base-room", template.StandardOptions{Name: "our-room"}); err != nil {
		t.Fatalf("fork the parent under a name of our own: %v", err)
	}
	// The template's parent name resolves to nothing we hold; naming ours does.
	if _, err := template.CreateStandardFromTemplate(ctx, gw, cat, "", "base-room-wide", template.StandardOptions{}); !errors.Is(err, storage.ErrTemplateParentMissing) {
		t.Fatalf("fork a variant whose template parent we hold under another name: %v", err)
	}
	v, err := template.CreateStandardFromTemplate(ctx, gw, cat, "", "base-room-wide", template.StandardOptions{Parent: "our-room"})
	if err != nil {
		t.Fatalf("fork the variant under our parent: %v", err)
	}
	if v.ParentStandardName == nil || *v.ParentStandardName != "our-room" {
		t.Fatalf("variant parent = %v, want our-room", v.ParentStandardName)
	}
}

// TestShippedStandardTemplatesCreateWhole is the build's real-write half of
// the dry run: every shipped standard template forks into the seeded
// catalogs, parents before variants, and each fork holds exactly the roles
// its template declares. A second pass forks nothing.
func TestShippedStandardTemplatesCreateWhole(t *testing.T) {
	gw := seeded(t)
	ctx := context.Background()
	forked, err := template.CreateAllStandards(ctx, gw, template.Shipped(), "")
	if err != nil {
		t.Fatalf("fork all: %v", err)
	}
	want := template.Shipped().List(template.KindStandard)
	if len(forked) != len(want) {
		t.Fatalf("forked %d standards, want %d", len(forked), len(want))
	}
	for _, tm := range want {
		checkForkMatches(t, gw, tm)
	}
	again, err := template.CreateAllStandards(ctx, gw, template.Shipped(), "")
	if err != nil || len(again) != 0 {
		t.Fatalf("a second pass forked %v (%v), want nothing", again, err)
	}
}

// TestAStandardFromATemplateIgnoresTheNextRelease: editing a fork, then booting a
// binary whose template changed, changes nothing in the fleet. The boot seed
// plants no standard and the fork pass leaves a held standard alone.
func TestAStandardFromATemplateIgnoresTheNextRelease(t *testing.T) {
	gw := seeded(t)
	ctx := context.Background()
	if _, err := template.CreateStandardFromTemplate(ctx, gw, fixture(t), "", "base-room", template.StandardOptions{}); err != nil {
		t.Fatalf("fork: %v", err)
	}
	label := "Our Room"
	if _, err := gw.UpdateStandard(ctx, "", "base-room", storage.StandardPatch{Label: &label}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	// The next release reworks the template: a new label, a role more.
	next, err := template.Load(map[string][]byte{"f.yaml": []byte(`templates:
  - kind: standard_template
    name: base-room
    label: Base Room Mk II
    standard:
      roles:
        - { name: main-display, label: Main Display, accepted_types: [display] }
        - { name: speakers, label: Speakers, quorum: 4, accepted_types: [speaker] }
`)})
	if err != nil {
		t.Fatalf("load next: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("reboot seed: %v", err)
	}
	if forked, err := template.CreateAllStandards(ctx, gw, next, ""); err != nil || len(forked) != 0 {
		t.Fatalf("the next release's fork pass forked %v (%v), want nothing", forked, err)
	}
	st, err := gw.GetStandard(ctx, "base-room")
	if err != nil || st.Label != "Our Room" {
		t.Fatalf("after the next release: %+v (%v), want the operator's label", st, err)
	}
	roles, _ := gw.ListSystemRoles(ctx, "standard", "base-room")
	if len(roles) != 3 {
		t.Errorf("after the next release: %d roles, want the fork's 3", len(roles))
	}
}

// jsonHas reports whether the JSON object raw holds key with the string value
// want.
func jsonHas(raw []byte, key, want string) bool {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	got, ok := m[key].(string)
	return ok && got == want
}

// checkForkMatches holds a forked standard to its template: the same roles
// (name, quorum, accepted types, pins, the alternate each joins), the same
// metric contract, and the same map. What the dev fleet's Map tab and KPI
// tiles read moved from the old seed into the templates, so this is their
// pin.
func checkForkMatches(t *testing.T, gw storage.Gateway, tm template.Template) {
	t.Helper()
	ctx := context.Background()
	st, err := gw.GetStandard(ctx, tm.Name)
	if err != nil {
		t.Fatalf("read %s: %v", tm.Name, err)
	}
	roles, err := gw.ListSystemRoles(ctx, "standard", tm.Name)
	if err != nil {
		t.Fatalf("roles of %s: %v", tm.Name, err)
	}
	got := map[string]string{}
	for _, r := range roles {
		got[r.Name] = roleKey(r.Quorum, r.AcceptedTypes, r.PinnedProducts, r.Alternate)
	}
	want := map[string]string{}
	for _, r := range tm.Standard.Roles {
		alt := ""
		if r.Choice != "" {
			alt = r.Choice + "/" + r.Alternate
		}
		want[r.Name] = roleKey(r.Quorate(), r.AcceptedTypes, r.PinnedProducts, alt)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s roles = %v, want %v", tm.Name, got, want)
	}
	var metrics, wantMetrics []string
	ms, err := gw.ListStandardMetrics(ctx, tm.Name)
	if err != nil {
		t.Fatalf("metrics of %s: %v", tm.Name, err)
	}
	for _, m := range ms {
		metrics = append(metrics, m.MetricTypeName)
	}
	for _, l := range tm.Standard.Metrics {
		wantMetrics = append(wantMetrics, l.Name)
	}
	sort.Strings(metrics)
	sort.Strings(wantMetrics)
	if !slices.Equal(metrics, wantMetrics) {
		t.Errorf("%s metric contract = %v, want %v", tm.Name, metrics, wantMetrics)
	}
	var wantMap []byte
	if tm.Standard.Map != nil {
		wantMap, _ = json.Marshal(tm.Standard.Map)
	}
	if !jsonEqual(st.Map, wantMap) {
		t.Errorf("%s map = %s, want %s", tm.Name, st.Map, wantMap)
	}
}

// roleKey renders the parts of a role a fork must carry, sets sorted.
func roleKey(quorum int, types, pins []string, alt string) string {
	ts := append([]string{}, types...)
	ps := append([]string{}, pins...)
	sort.Strings(ts)
	sort.Strings(ps)
	return fmt.Sprintf("quorum %d accepts %v pins %v alternate %q", quorum, ts, ps, alt)
}

func jsonEqual(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
