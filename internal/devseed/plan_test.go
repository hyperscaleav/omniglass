package devseed_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
)

// SeedPlan lands everything the plan planned: the systems in their rooms, the
// components in those rooms, each role staffed by its product's components, and
// the simulated endpoints on the components.
func TestSeedPlanLandsThePlannedRows(t *testing.T) {
	t.Parallel()
	ctx, gw, conn := openSeededGateway(t)
	p := corporatePlan(t, 20)
	res, err := devseed.SeedPlan(ctx, gw, "", p, devseed.PlanOptions{Endpoints: true})
	if err != nil {
		t.Fatal(err)
	}

	var wantSystems, wantComponents, wantRoles int
	type roleKey struct{ system, role, product string }
	wantStaffing := map[roleKey]int{}
	wantRoom := map[string]string{}
	for _, site := range p.Sites {
		for _, s := range site.Systems {
			wantSystems++
			wantRoom[s.Name] = s.Room
			for _, fill := range s.Roles {
				wantComponents += fill.Count
				wantRoles += fill.Count
				wantStaffing[roleKey{s.Name, fill.Role, fill.Product}] = fill.Count
			}
		}
	}
	if wantSystems != 20 || wantComponents != p.Totals.Components {
		t.Fatalf("plan has %d systems and %d role fills, want 20 and %d components", wantSystems, wantComponents, p.Totals.Components)
	}
	if res.Systems != wantSystems || res.Components != wantComponents || res.Roles != wantRoles {
		t.Fatalf("result = %d systems, %d components, %d roles; want %d, %d, %d", res.Systems, res.Components, res.Roles, wantSystems, wantComponents, wantRoles)
	}

	// Systems: each planned system exists, in its planned room, and no other
	// system was made.
	gotRoom := map[string]string{}
	rows, err := conn.Query(ctx, `select s.name, l.name from system s join location l on l.id = s.location_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name, room string
		if err := rows.Scan(&name, &room); err != nil {
			t.Fatal(err)
		}
		gotRoom[name] = room
	}
	rows.Close()
	if fmt.Sprint(sortedPairs(gotRoom)) != fmt.Sprint(sortedPairs(wantRoom)) {
		t.Fatalf("systems and rooms = %v, want %v", sortedPairs(gotRoom), sortedPairs(wantRoom))
	}

	// Role assignments: each (system, role) is staffed by the planned number of
	// components of the planned product.
	gotStaffing := map[roleKey]int{}
	rows, err = conn.Query(ctx, `
		select s.name, r.name, p.name, count(*)
		from system_role_assignment a
		join system s on s.id = a.system_id
		join system_role r on r.id = a.role_id
		join component c on c.id = a.component_id
		join product p on p.id = c.product_id
		group by 1, 2, 3`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k roleKey
		var n int
		if err := rows.Scan(&k.system, &k.role, &k.product, &n); err != nil {
			t.Fatal(err)
		}
		gotStaffing[k] = n
	}
	rows.Close()
	if len(gotStaffing) != len(wantStaffing) {
		t.Fatalf("%d staffed roles, want %d", len(gotStaffing), len(wantStaffing))
	}
	for k, want := range wantStaffing {
		if gotStaffing[k] != want {
			t.Errorf("%s role %s: %d x %s, want %d", k.system, k.role, gotStaffing[k], k.product, want)
		}
	}

	// Components: only the planned ones, each a member of the system it staffs
	// and placed in that system's room.
	var components, misplaced, unmembered int
	if err := conn.QueryRow(ctx, `select count(*) from component`).Scan(&components); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `
		select count(*) from system_role_assignment a
		join system s on s.id = a.system_id
		join component c on c.id = a.component_id
		where c.location_id <> s.location_id`).Scan(&misplaced); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `
		select count(*) from system_role_assignment a
		where not exists (select 1 from system_member m where m.system_id = a.system_id and m.component_id = a.component_id)`).Scan(&unmembered); err != nil {
		t.Fatal(err)
	}
	if components != wantComponents || misplaced != 0 || unmembered != 0 {
		t.Errorf("%d components (want %d), %d staffing a system from another room, %d staffing without membership", components, wantComponents, misplaced, unmembered)
	}

	// Endpoints: the count the seed reports is the count in the database, and
	// every one sits on a planned component.
	var endpoints, orphaned int
	if err := conn.QueryRow(ctx, `select count(*) from endpoint`).Scan(&endpoints); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `select count(*) from endpoint e where not exists (select 1 from component c where c.id = e.component)`).Scan(&orphaned); err != nil {
		t.Fatal(err)
	}
	if res.Endpoints == 0 || endpoints != res.Endpoints || orphaned != 0 {
		t.Errorf("%d endpoints in the database, result says %d, %d on no component; want equal, non-zero, none", endpoints, res.Endpoints, orphaned)
	}
}

// The same plan seeded into two fresh databases mints the same component
// names and the same endpoint addresses, so a scale lab run is repeatable.
func TestSeedPlanIsDeterministic(t *testing.T) {
	t.Parallel()
	read := func() []string {
		ctx, gw, conn := openSeededGateway(t)
		if _, err := devseed.SeedPlan(ctx, gw, "", corporatePlan(t, 5), devseed.PlanOptions{Endpoints: true}); err != nil {
			t.Fatal(err)
		}
		rows, err := conn.Query(ctx, `
			select s.name || '/' || r.name || '/' || c.name || '/' || coalesce(e.params->>'target', '')
			from system_role_assignment a
			join system s on s.id = a.system_id
			join system_role r on r.id = a.role_id
			join component c on c.id = a.component_id
			left join endpoint e on e.component = c.id
			order by 1`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return out
	}
	a, b := read(), read()
	if len(a) == 0 || fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatalf("two seeds of one plan differ (%d vs %d rows)", len(a), len(b))
	}
}

func sortedPairs(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}
