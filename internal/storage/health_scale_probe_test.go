package storage_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestHealthRecomputeScaleProbe is the measurement behind #1562: how the health
// recompute an AssignRole pays grows with the fleet. It builds the corporate
// plan's location tree (campus, building, floor, room, one system per room) at
// each size, staffs every system with as many components as the plan gives
// it, row by row through AssignRole, and reports where the time went, along
// with the recompute alone (the one AssignRole runs), sampled as the fleet
// fills.
//
// It is a probe, not a gate: set HEALTH_MEASURE to the sizes (e.g. 100,1000)
// to run it.
func TestHealthRecomputeScaleProbe(t *testing.T) {
	// Serial: a timing probe; running beside other tests would skew the numbers it exists to report.
	sizes := os.Getenv("HEALTH_MEASURE")
	if sizes == "" {
		t.Skip("set HEALTH_MEASURE=100,1000 to measure")
	}
	for _, f := range strings.Split(sizes, ",") {
		size, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			t.Fatalf("HEALTH_MEASURE %q: %v", f, err)
		}
		t.Run(strconv.Itoa(size), func(t *testing.T) { probeHealthAtSize(t, size) })
	}
}

func probeHealthAtSize(t *testing.T, size int) {
	ctx := context.Background()
	plan := probePlan(t, size)
	timer := newStmtTimer()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithQueryTracer(timer))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "probe-std", Label: "Probe"}); err != nil {
		t.Fatalf("standard: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", "probe-std", storage.SystemRoleSpec{
		Name: "member", Label: "Member", Quorum: 1, AcceptedTypes: []string{"video-bar"}, Impact: "degraded",
	}); err != nil {
		t.Fatalf("role: %v", err)
	}
	bar := storagetest.MintProduct(t, ctx, gw, "video-bar").Name

	location := func(name, kind string, parent *string) {
		if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: name, LocationType: kind, ParentName: parent}, all); err != nil {
			t.Fatalf("location %s: %v", name, err)
		}
	}
	type staffing struct{ system, systemID, component, componentID string }
	var staffed []staffing
	std := "probe-std"
	start := time.Now()
	for _, site := range plan.Sites {
		location(site.Name, "campus", nil)
		for _, b := range site.Buildings {
			location(b.Name, "building", &site.Name)
			for fl := 1; fl <= b.Floors; fl++ {
				location(fmt.Sprintf("%s-%d", b.Name, fl), "floor", &b.Name)
			}
		}
		for _, s := range site.Systems {
			location(s.Room, "room", &s.Floor)
			room := s.Room
			sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: s.Name, StandardID: &std, LocationName: &room}, all, all)
			if err != nil {
				t.Fatalf("system %s: %v", s.Name, err)
			}
			n := 0
			for _, c := range s.Components {
				n += c.Count
			}
			for i := range n {
				name := fmt.Sprintf("%s-c%d", s.Name, i)
				c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name, ProductName: &bar}, all, all, all, all)
				if err != nil {
					t.Fatalf("component %s: %v", name, err)
				}
				staffed = append(staffed, staffing{s.Name, sys.ID, name, c.ID})
			}
		}
	}
	built := time.Since(start)

	// Every step-th AssignRole is followed by the same recompute again, timed
	// on its own, so the sample tracks the fleet as it grows the way the
	// recompute inside the write does. The extra pass records nothing (the
	// verdicts it computes are already recorded), and its statements are
	// left out of the staffing totals.
	const samples = 300
	step := max(len(staffed)/samples, 1)
	var sampled, assigned time.Duration
	n := 0
	timer.reset()
	for i, s := range staffed {
		begin := time.Now()
		if err := gw.AssignRole(ctx, "", s.system, "member", s.component, all, all, all, all); err != nil {
			t.Fatalf("assign %s: %v", s.component, err)
		}
		assigned += time.Since(begin)
		if i%step != 0 {
			continue
		}
		timer.pause()
		begin = time.Now()
		if err := gw.ExportRecomputeStaffing(ctx, s.componentID, s.systemID); err != nil {
			t.Fatalf("recompute %s: %v", s.component, err)
		}
		sampled += time.Since(begin)
		n++
		timer.resume()
	}
	t.Logf("%d systems, %d sites, %d AssignRole: built in %s, staffed in %s (%s per AssignRole)",
		plan.Totals.Systems, len(plan.Sites), len(staffed), built.Round(time.Millisecond),
		assigned.Round(time.Millisecond), (assigned / time.Duration(len(staffed))).Round(time.Microsecond))
	timer.report(t, len(staffed), 8)

	t.Logf("health recompute per AssignRole: %s (mean of %d, sampled through the staffing)",
		(sampled / time.Duration(n)).Round(time.Microsecond), n)
}

func probePlan(t *testing.T, size int) *scenario.Plan {
	t.Helper()
	m, err := scenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := scenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	p, err := scenario.Resolve(m, cat, size)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// stmtTimer is a pgx tracer that totals each statement's count and time, so the
// probe can say where the time went and how many of each one a write issued.
type stmtTimer struct {
	mu     sync.Mutex
	stat   map[string]*stmtStat
	paused bool
}

type stmtStat struct {
	n int
	d time.Duration
}

type stmtStartKey struct{}

type stmtStart struct {
	sql string
	at  time.Time
}

func newStmtTimer() *stmtTimer { return &stmtTimer{stat: map[string]*stmtStat{}} }

func (s *stmtTimer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, stmtStartKey{}, stmtStart{sql: d.SQL, at: time.Now()})
}

func (s *stmtTimer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	v, ok := ctx.Value(stmtStartKey{}).(stmtStart)
	if !ok {
		return
	}
	key := strings.Join(strings.Fields(v.sql), " ")
	if len(key) > 100 {
		key = key[:100]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused {
		return
	}
	st := s.stat[key]
	if st == nil {
		st = &stmtStat{}
		s.stat[key] = st
	}
	st.n++
	st.d += time.Since(v.at)
}

func (s *stmtTimer) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stat = map[string]*stmtStat{}
}

func (s *stmtTimer) pause()  { s.setPaused(true) }
func (s *stmtTimer) resume() { s.setPaused(false) }

func (s *stmtTimer) setPaused(p bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = p
}

// report logs the top statements by total time, each with its count per write
// and its mean per call.
func (s *stmtTimer) report(t *testing.T, writes, top int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	type row struct {
		sql string
		st  stmtStat
	}
	var rows []row
	var total time.Duration
	for k, v := range s.stat {
		rows = append(rows, row{k, *v})
		total += v.d
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].st.d > rows[j].st.d })
	t.Logf("statements: %s in total", total.Round(time.Millisecond))
	for i, r := range rows {
		if i == top {
			break
		}
		t.Logf("  %9s %7dx %5.1f/write %8s/call  %s", r.st.d.Round(time.Millisecond), r.st.n,
			float64(r.st.n)/float64(writes), (r.st.d / time.Duration(r.st.n)).Round(time.Microsecond), r.sql)
	}
}
