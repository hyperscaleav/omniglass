package devseed_test

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

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// stmtTimer is a pgx tracer that totals the time and count of each statement,
// so a measurement can say where a seed's time goes.
type stmtTimer struct {
	mu    sync.Mutex
	start map[context.Context]time.Time
	stat  map[string]*stmtStat
	total int
}

type stmtStat struct {
	n int
	d time.Duration
}

type stmtKey struct{}
type stmtVal struct {
	sql string
	at  time.Time
}

func newStmtTimer() *stmtTimer { return &stmtTimer{stat: map[string]*stmtStat{}} }

func (s *stmtTimer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, stmtKey{}, stmtVal{sql: d.SQL, at: time.Now()})
}

func (s *stmtTimer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	v, ok := ctx.Value(stmtKey{}).(stmtVal)
	if !ok {
		return
	}
	key := strings.Join(strings.Fields(v.sql), " ")
	if len(key) > 90 {
		key = key[:90]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stat[key]
	if st == nil {
		st = &stmtStat{}
		s.stat[key] = st
	}
	st.n++
	st.d += time.Since(v.at)
	s.total++
}

func (s *stmtTimer) report(t *testing.T, top int) {
	type row struct {
		sql string
		st  stmtStat
	}
	var rows []row
	var all time.Duration
	for k, v := range s.stat {
		rows = append(rows, row{k, *v})
		all += v.d
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].st.d > rows[j].st.d })
	t.Logf("statements: %d round trips, %s in statements", s.total, all.Round(time.Millisecond))
	for i, r := range rows {
		if i == top {
			break
		}
		t.Logf("  %8s %6dx  %s", r.st.d.Round(time.Millisecond), r.st.n, r.sql)
	}
}

// TestPlanSeedMeasure is the measurement behind the scale lab's seeding budget:
// the corporate plan seeded row by row, with where the time goes. It is a probe,
// not a gate: set PLAN_MEASURE to the sizes (e.g. 100,1000) to run it.
func TestPlanSeedMeasure(t *testing.T) {
	// Serial: a timing probe; running beside other tests would skew the numbers it exists to report.
	sizes := os.Getenv("PLAN_MEASURE")
	if sizes == "" {
		t.Skip("set PLAN_MEASURE=100,1000 to measure")
	}
	for _, f := range strings.Split(sizes, ",") {
		size, _ := strconv.Atoi(f)
		t.Run(fmt.Sprintf("%d", size), func(t *testing.T) {
			p := corporatePlan(t, size)
			dsn := devseed.BootSeededDSN(t)
			ctx := context.Background()
			tm := newStmtTimer()
			gw, err := storage.NewPG(ctx, dsn, devseed.SecretProvider(), storage.WithQueryTracer(tm))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(gw.Close)
			start := time.Now()
			res, err := devseed.SeedPlan(ctx, gw, "", p, devseed.PlanOptions{Endpoints: true})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%d systems (%d locations, %d components, %d endpoints) in %s", res.Systems, res.Locations, res.Components, res.Endpoints, time.Since(start).Round(time.Millisecond))
			for _, ph := range res.Phases {
				t.Logf("  phase %-14s %s", ph.Name, ph.Took.Round(time.Millisecond))
			}
			tm.report(t, 14)
			gw.Close() // the backends flush their statistics as they end
			time.Sleep(2 * time.Second)
			conn, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close(ctx) }()
			// Postgres' own account of which tables the seed wrote.
			for _, q := range strings.Split(os.Getenv("PLAN_DUMP"), ";") {
				if strings.TrimSpace(q) == "" {
					continue
				}
				dr, err := conn.Query(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				for dr.Next() {
					vals, _ := dr.Values()
					t.Logf("  dump %v", vals)
				}
				dr.Close()
			}
			rs, err := conn.Query(ctx, `select relname, n_tup_ins, n_tup_upd, n_tup_del from pg_stat_user_tables where n_tup_ins+n_tup_upd+n_tup_del > 0 order by n_tup_ins desc`)
			if err != nil {
				t.Fatal(err)
			}
			defer rs.Close()
			for rs.Next() {
				var rel string
				var i, u, d int64
				_ = rs.Scan(&rel, &i, &u, &d)
				t.Logf("  table %-34s ins %6d upd %6d del %6d", rel, i, u, d)
			}
		})
	}
}

func corporatePlan(t testing.TB, size int) *scenario.Plan {
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
