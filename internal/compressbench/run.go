package compressbench

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/hyperscaleav/omniglass/internal/labfootprint"
)

// Server is one running Postgres 18 container a candidate's image provides.
type Server struct {
	Image     Image
	Container *tcpostgres.PostgresContainer
	base      *pgx.ConnConfig
}

// Start runs img on a random port. The postgres module sets its own server
// command, which replaces the image's, so the preload arguments are passed
// here rather than relied on from the image.
func Start(ctx context.Context, img Image) (*Server, error) {
	c, err := tcpostgres.Run(ctx, img.Ref,
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("bench"),
		testcontainers.WithCmd(append([]string{"postgres"}, img.Args...)...),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(2*time.Minute)),
	)
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", img.Ref, err)
	}
	dsn, err := c.ConnectionString(ctx, "sslmode=disable", "timezone=UTC")
	if err != nil {
		return nil, err
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	return &Server{Image: img, Container: c, base: cfg}, nil
}

// Stop removes the container.
func (s *Server) Stop(ctx context.Context) error {
	return testcontainers.TerminateContainer(s.Container)
}

// Connect opens a connection to database db.
func (s *Server) Connect(ctx context.Context, db string) (*pgx.Conn, error) {
	cfg := s.base.Copy()
	cfg.Database = db
	return pgx.ConnectConfig(ctx, cfg)
}

func (s *Server) admin(ctx context.Context, sql string) error {
	conn, err := s.Connect(ctx, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, sql)
	return err
}

// Template is spec's samples staged once in a template database on a server,
// which every candidate run copies from.
func (s *Server) Template(ctx context.Context, spec Spec, samples []Sample) (string, error) {
	name := "tpl_" + sqlIdent(spec.Label())
	if err := s.admin(ctx, "drop database if exists "+name+" with (force)"); err != nil {
		return "", err
	}
	if err := s.admin(ctx, "create database "+name); err != nil {
		return "", err
	}
	conn, err := s.Connect(ctx, name)
	if err != nil {
		return "", err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, stagingDDL); err != nil {
		return "", err
	}
	i := 0
	_, err = conn.CopyFrom(ctx, pgx.Identifier{"staging", "sample"}, []string{"seq", "series_id", "ts", "value"},
		pgx.CopyFromFunc(func() ([]any, error) {
			if i == len(samples) {
				return nil, nil
			}
			x := samples[i]
			i++
			return []any{int64(i), x.SeriesID, x.TS, x.Value}, nil
		}))
	if err != nil {
		return "", fmt.Errorf("stage: %w", err)
	}
	// Frozen and analyzed once here, so no candidate run pays to vacuum the
	// staging rows it copies.
	if _, err := conn.Exec(ctx, "vacuum (freeze, analyze) staging.sample"); err != nil {
		return "", err
	}
	return name, nil
}

// DropTemplate removes a template database.
func (s *Server) DropTemplate(ctx context.Context, name string) error {
	return s.admin(ctx, "drop database if exists "+name+" with (force)")
}

// Measure is what one run of one candidate measured.
type Measure struct {
	Samples     int
	Bytes       int64
	Compress    time.Duration
	ReadRaw     time.Duration
	ReadBuckets time.Duration
	// Cagg is the continuous aggregate's own measure, when the candidate has one:
	// its bytes, its refresh as Compress, and its bucketed read.
	Cagg *Measure
	// Blocks marks a block candidate, whose SummaryBytes (bytes of the summary
	// columns) and CompressedRows (share of rows whose samples Postgres
	// compressed) were measured.
	Blocks         bool
	Summarized     bool
	SummaryBytes   int64
	CompressedRows float64
	// DaySpans is how each series-day was stored, when the candidate says.
	DaySpans []DaySpan
	// Merged marks a run that landed one late sample: Merge is how long it
	// took and MergeWAL the WAL bytes it wrote, from a checkpoint.
	Merged   bool
	Merge    time.Duration
	MergeWAL int64
}

// Read is the window and bucketing every candidate's reads are held to.
type Read struct {
	Series      int64
	RawFrom     time.Time
	RawTo       time.Time
	BucketFrom  time.Time
	BucketTo    time.Time
	BucketWidth time.Duration
	Reps        int
}

// ReadFor is the reads at spec: the last 24 h raw and up to 30 d bucketed, of
// the noisiest series (the one every format compresses worst).
func ReadFor(spec Spec, reps int) Read {
	series := int64(len(kinds))
	if spec.Series < len(kinds) {
		series = int64(spec.Series)
	}
	from := spec.start()
	if spec.Span > 30*24*time.Hour {
		from = spec.End().Add(-30 * 24 * time.Hour)
	}
	return Read{
		Series: series, RawFrom: spec.End().Add(-24 * time.Hour), RawTo: spec.End(),
		BucketFrom: from, BucketTo: spec.End(), BucketWidth: BucketWidth(spec.Cadence), Reps: reps,
	}
}

// RunOnce builds cand in a fresh copy of tpl, measures it, and, when verify,
// holds every read and the full round trip to samples.
func (s *Server) RunOnce(ctx context.Context, tpl string, run int, cand Candidate, spec Spec, samples []Sample, rd Read, verify bool, log io.Writer) (Measure, error) {
	db := sqlIdent(fmt.Sprintf("run_%s_%s_%d", cand.Name, spec.Label(), run))
	if err := s.admin(ctx, "drop database if exists "+db+" with (force)"); err != nil {
		return Measure{}, err
	}
	if err := s.admin(ctx, "create database "+db+" template "+tpl); err != nil {
		return Measure{}, err
	}
	defer func() { _ = s.admin(context.WithoutCancel(ctx), "drop database if exists "+db+" with (force)") }()
	conn, err := s.Connect(ctx, db)
	if err != nil {
		return Measure{}, err
	}
	defer conn.Close(ctx)
	if cand.Image.Extension != "" {
		if _, err := conn.Exec(ctx, "create extension "+cand.Image.Extension); err != nil {
			return Measure{}, err
		}
	}
	base, err := size(ctx, conn)
	if err != nil {
		return Measure{}, err
	}
	if err := execAll(ctx, conn, cand.Setup); err != nil {
		return Measure{}, fmt.Errorf("%s setup: %w", cand.Name, err)
	}
	if err := execAll(ctx, conn, cand.Load); err != nil {
		return Measure{}, fmt.Errorf("%s load: %w", cand.Name, err)
	}
	t0 := time.Now()
	if err := execAll(ctx, conn, cand.Compress); err != nil {
		return Measure{}, fmt.Errorf("%s compress: %w", cand.Name, err)
	}
	m := Measure{Samples: len(samples), Compress: time.Since(t0)}
	if _, err := conn.Exec(ctx, "vacuum analyze"); err != nil {
		return Measure{}, err
	}
	after, err := size(ctx, conn)
	if err != nil {
		return Measure{}, err
	}
	m.Bytes = after - base
	if verify {
		if err := verifyCandidate(ctx, conn, cand, samples, rd); err != nil {
			return Measure{}, fmt.Errorf("%s at %s: %w", cand.Name, spec.Label(), err)
		}
	}
	if m.ReadRaw, err = timeRead(ctx, conn, cand.ReadRaw, rd, false); err != nil {
		return Measure{}, err
	}
	if m.ReadBuckets, err = timeRead(ctx, conn, cand.ReadBuckets, rd, true); err != nil {
		return Measure{}, err
	}
	if cand.Cagg != nil {
		cm, err := measureCagg(ctx, conn, cand, samples, rd, after, verify)
		if err != nil {
			return Measure{}, fmt.Errorf("%s: %w", cand.Cagg.Name, err)
		}
		m.Cagg = &cm
	}
	if cand.SummaryBytes != "" {
		m.Summarized = true
		if err := conn.QueryRow(ctx, cand.SummaryBytes).Scan(&m.SummaryBytes); err != nil {
			return Measure{}, fmt.Errorf("%s summary bytes: %w", cand.Name, err)
		}
	}
	if cand.CompressedRows != "" {
		m.Blocks = true
		if err := conn.QueryRow(ctx, cand.CompressedRows).Scan(&m.CompressedRows); err != nil {
			return Measure{}, fmt.Errorf("%s compressed rows: %w", cand.Name, err)
		}
	}
	if cand.DaySpans != "" {
		if m.DaySpans, err = daySpans(ctx, conn, cand.DaySpans); err != nil {
			return Measure{}, fmt.Errorf("%s day spans: %w", cand.Name, err)
		}
		if verify && cand.WantDaySpans != nil {
			if err := sameDaySpans(cand.WantDaySpans(samples), m.DaySpans); err != nil {
				return Measure{}, fmt.Errorf("%s at %s: %w", cand.Name, spec.Label(), err)
			}
		}
	}
	if cand.Merge != "" {
		late := Late(samples, rd.Series)
		if m.Merge, m.MergeWAL, err = merge(ctx, conn, cand.Merge, late); err != nil {
			return Measure{}, fmt.Errorf("%s late merge: %w", cand.Name, err)
		}
		m.Merged = true
		if verify {
			if err := verifyMerged(ctx, conn, cand, WithLate(samples, late)); err != nil {
				return Measure{}, fmt.Errorf("%s at %s after a late merge: %w", cand.Name, spec.Label(), err)
			}
		}
	}
	if log != nil {
		fmt.Fprintf(log, "  %-12s %-6s run %d: %d B, %.2f B/sample, compress %s\n",
			cand.Name, spec.Label(), run, m.Bytes, float64(m.Bytes)/float64(m.Samples), m.Compress.Round(time.Millisecond))
	}
	return m, nil
}

// merge lands late with cand's Merge and is its time and the WAL bytes it
// wrote. A checkpoint first makes every page it touches write a full-page
// image, so the WAL is the worst case a closed span costs, run after run.
func merge(ctx context.Context, conn *pgx.Conn, sql string, late Sample) (time.Duration, int64, error) {
	if _, err := conn.Exec(ctx, "checkpoint"); err != nil {
		return 0, 0, err
	}
	var before, after string
	if err := conn.QueryRow(ctx, "select pg_current_wal_lsn()::text").Scan(&before); err != nil {
		return 0, 0, err
	}
	t0 := time.Now()
	tag, err := conn.Exec(ctx, sql, late.SeriesID, late.TS, late.Value)
	d := time.Since(t0)
	if err != nil {
		return 0, 0, err
	}
	if tag.RowsAffected() != 1 {
		return 0, 0, fmt.Errorf("%d rows written, want 1", tag.RowsAffected())
	}
	if err := conn.QueryRow(ctx, "select pg_current_wal_lsn()::text").Scan(&after); err != nil {
		return 0, 0, err
	}
	var wal int64
	err = conn.QueryRow(ctx, "select pg_wal_lsn_diff($1::pg_lsn, $2::pg_lsn)::int8", after, before).Scan(&wal)
	return d, wal, err
}

// verifyMerged holds the candidate, after a late merge, to samples with the
// late one in: its invariants, the full round trip and every hour's summary.
func verifyMerged(ctx context.Context, conn *pgx.Conn, cand Candidate, samples []Sample) error {
	if err := check(ctx, conn, cand); err != nil {
		return err
	}
	if err := roundTrip(ctx, conn, cand.ReadAll, samples); err != nil {
		return err
	}
	return hourly(ctx, conn, cand, samples)
}

func check(ctx context.Context, conn *pgx.Conn, cand Candidate) error {
	if cand.Check == "" {
		return nil
	}
	var bad int64
	if err := conn.QueryRow(ctx, cand.Check).Scan(&bad); err != nil {
		return err
	}
	if bad != 0 {
		return fmt.Errorf("%d rows break the format's invariants", bad)
	}
	return nil
}

// hourly holds every stored hourly summary to the one samples recompute.
func hourly(ctx context.Context, conn *pgx.Conn, cand Candidate, samples []Sample) error {
	if cand.HourlyAll == "" {
		return nil
	}
	rows, err := conn.Query(ctx, cand.HourlyAll)
	if err != nil {
		return err
	}
	got, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Summary, error) {
		var s Summary
		err := r.Scan(&s.SeriesID, &s.Hour, &s.Count, &s.Min, &s.Max, &s.Sum, &s.First, &s.Last)
		return s, err
	})
	if err != nil {
		return err
	}
	if err := SameSummaries(HourlySummaries(samples), got); err != nil {
		return fmt.Errorf("hourly summaries: %w", err)
	}
	return nil
}

func measureCagg(ctx context.Context, conn *pgx.Conn, cand Candidate, samples []Sample, rd Read, before int64, verify bool) (Measure, error) {
	if err := execAll(ctx, conn, cand.Cagg.Setup); err != nil {
		return Measure{}, err
	}
	t0 := time.Now()
	if err := execAll(ctx, conn, cand.Cagg.Refresh); err != nil {
		return Measure{}, err
	}
	m := Measure{Samples: len(samples), Compress: time.Since(t0)}
	if _, err := conn.Exec(ctx, "vacuum analyze"); err != nil {
		return Measure{}, err
	}
	after, err := size(ctx, conn)
	if err != nil {
		return Measure{}, err
	}
	m.Bytes = after - before
	if verify {
		if err := hourly(ctx, conn, Candidate{HourlyAll: cand.Cagg.HourlyAll}, samples); err != nil {
			return Measure{}, err
		}
		got, err := readBuckets(ctx, conn, cand.Cagg.ReadBuckets, rd)
		if err != nil {
			return Measure{}, err
		}
		if err := SameBuckets(Buckets(samples, rd.Series, rd.BucketFrom, rd.BucketTo, rd.BucketWidth), got); err != nil {
			return Measure{}, fmt.Errorf("bucketed read: %w", err)
		}
	}
	if m.ReadBuckets, err = timeRead(ctx, conn, cand.Cagg.ReadBuckets, rd, true); err != nil {
		return Measure{}, err
	}
	return m, nil
}

func execAll(ctx context.Context, conn *pgx.Conn, stmts []string) error {
	for _, q := range stmts {
		if _, err := conn.Exec(ctx, q); err != nil {
			return fmt.Errorf("%w\n%s", err, q)
		}
	}
	return nil
}

// sizeSQL is every table's and materialized view's total size (heap, TOAST,
// indexes) outside the catalogs and the staging rows. A compressor's companion
// and chunk tables are ordinary tables in its own schemas, so they count.
const sizeSQL = `select coalesce(sum(pg_total_relation_size(c.oid)), 0)::int8
from pg_class c join pg_namespace n on n.oid = c.relnamespace
where c.relkind in ('r', 'm')
	and n.nspname not in ('pg_catalog', 'information_schema', 'staging')
	and n.nspname not like 'pg_toast%' and n.nspname not like 'pg_temp%'`

func size(ctx context.Context, conn *pgx.Conn) (int64, error) {
	var n int64
	err := conn.QueryRow(ctx, sizeSQL).Scan(&n)
	return n, err
}

func verifyCandidate(ctx context.Context, conn *pgx.Conn, cand Candidate, samples []Sample, rd Read) error {
	if err := check(ctx, conn, cand); err != nil {
		return err
	}
	if err := hourly(ctx, conn, cand, samples); err != nil {
		return err
	}
	raw, err := readRaw(ctx, conn, cand.ReadRaw, rd)
	if err != nil {
		return err
	}
	if err := SamePoints(RawWindow(samples, rd.Series, rd.RawFrom, rd.RawTo), raw); err != nil {
		return fmt.Errorf("24 h raw read: %w", err)
	}
	b, err := readBuckets(ctx, conn, cand.ReadBuckets, rd)
	if err != nil {
		return err
	}
	if err := SameBuckets(Buckets(samples, rd.Series, rd.BucketFrom, rd.BucketTo, rd.BucketWidth), b); err != nil {
		return fmt.Errorf("bucketed read: %w", err)
	}
	return roundTrip(ctx, conn, cand.ReadAll, samples)
}

// roundTrip streams every sample back and holds it to samples: same order,
// same bits, duplicates in landing order.
func roundTrip(ctx context.Context, conn *pgx.Conn, sql string, samples []Sample) error {
	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return err
	}
	defer rows.Close()
	i := 0
	var s Sample
	for rows.Next() {
		if err := rows.Scan(&s.SeriesID, &s.TS, &s.Value); err != nil {
			return err
		}
		if i >= len(samples) {
			return fmt.Errorf("round trip: more than %d samples", len(samples))
		}
		w := samples[i]
		if s.SeriesID != w.SeriesID || !s.TS.Equal(w.TS) || SamePoints([]Point{{w.TS, w.Value}}, []Point{{s.TS, s.Value}}) != nil {
			return fmt.Errorf("round trip: sample %d is series %d %s %v, want series %d %s %v", i,
				s.SeriesID, s.TS.UTC().Format(time.RFC3339Nano), s.Value, w.SeriesID, w.TS.UTC().Format(time.RFC3339Nano), w.Value)
		}
		i++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if i != len(samples) {
		return fmt.Errorf("round trip: %d samples, want %d", i, len(samples))
	}
	return nil
}

func interval(d time.Duration) string { return fmt.Sprintf("%d seconds", int64(d/time.Second)) }

func readRaw(ctx context.Context, conn *pgx.Conn, sql string, rd Read) ([]Point, error) {
	rows, err := conn.Query(ctx, sql, rd.Series, rd.RawFrom, rd.RawTo)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Point, error) {
		var p Point
		err := r.Scan(&p.TS, &p.Value)
		return p, err
	})
}

func readBuckets(ctx context.Context, conn *pgx.Conn, sql string, rd Read) ([]Bucket, error) {
	rows, err := conn.Query(ctx, sql, rd.Series, rd.BucketFrom, rd.BucketTo, interval(rd.BucketWidth))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Bucket, error) {
		var b Bucket
		err := r.Scan(&b.Start, &b.Count, &b.Min, &b.Max, &b.Avg)
		return b, err
	})
}

// timeRead is the median of rd.Reps timed reads after one untimed warmup.
func timeRead(ctx context.Context, conn *pgx.Conn, sql string, rd Read, buckets bool) (time.Duration, error) {
	read := func() error {
		var err error
		if buckets {
			_, err = readBuckets(ctx, conn, sql, rd)
		} else {
			_, err = readRaw(ctx, conn, sql, rd)
		}
		return err
	}
	if err := read(); err != nil {
		return 0, err
	}
	ds := make([]time.Duration, 0, rd.Reps)
	for range rd.Reps {
		t0 := time.Now()
		if err := read(); err != nil {
			return 0, err
		}
		ds = append(ds, time.Since(t0))
	}
	return time.Duration(labfootprint.LatencyStat(ds).Median * float64(time.Millisecond)), nil
}

func daySpans(ctx context.Context, conn *pgx.Conn, sql string) ([]DaySpan, error) {
	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DaySpan, error) {
		var d DaySpan
		var n int64
		err := r.Scan(&d.SeriesID, &d.Day, &d.Span, &n)
		d.Day, d.Count = d.Day.UTC(), int(n)
		return d, err
	})
}

// sameDaySpans holds the stored spans to the rule's: every series-day once, at
// the span the rule gives it, with all its samples.
func sameDaySpans(want, got []DaySpan) error {
	if len(got) != len(want) {
		return fmt.Errorf("%d series-days stored, want %d", len(got), len(want))
	}
	for i, w := range want {
		if g := got[i]; g.SeriesID != w.SeriesID || !g.Day.Equal(w.Day) || g.Span != w.Span || g.Count != w.Count {
			return fmt.Errorf("series-day %d: got %+v, want %+v", i, g, w)
		}
	}
	return nil
}
