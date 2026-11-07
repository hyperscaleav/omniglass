package compressbench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	tcexec "github.com/testcontainers/testcontainers-go/exec"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Probe is one fit question asked of one extension: the statement or command
// run, and what the server answered.
type Probe struct {
	Question  string `json:"question"`
	Candidate string `json:"candidate"`
	Step      string `json:"step"`
	Outcome   string `json:"outcome"`
}

// probeSpec is the probes' dataset: three days of five series, every day
// sealed, so every partition and chunk compresses.
var probeSpec = Spec{Cadence: time.Minute, Series: 5, Span: 72 * time.Hour, Seed: 1516}

// probeEnv is one extension's probe database, staged with probeSpec's samples.
type probeEnv struct {
	sv      *Server
	tpl     string
	name    string
	samples []Sample
	out     *[]Probe
	log     io.Writer
}

func (e *probeEnv) record(q, step, outcome string) {
	p := Probe{Question: q, Candidate: e.name, Step: step, Outcome: outcome}
	*e.out = append(*e.out, p)
	if e.log != nil {
		fmt.Fprintf(e.log, "  %s %s: %s\n    -> %s\n", q, e.name, step, outcome)
	}
}

// fresh is a new database from the probe template, with the extension.
func (e *probeEnv) fresh(ctx context.Context, db string) (*pgx.Conn, error) {
	if err := e.sv.admin(ctx, "drop database if exists "+db+" with (force)"); err != nil {
		return nil, err
	}
	if err := e.sv.admin(ctx, "create database "+db+" template "+e.tpl); err != nil {
		return nil, err
	}
	conn, err := e.sv.Connect(ctx, db)
	if err != nil {
		return nil, err
	}
	_, err = conn.Exec(ctx, "create extension "+e.sv.Image.Extension)
	return conn, err
}

// sqlState is err's SQLSTATE and message, or "accepted" when err is nil.
func sqlState(err error) string {
	if err == nil {
		return "accepted"
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return fmt.Sprintf("refused, SQLSTATE %s: %s", pg.Code, pg.Message)
	}
	return "error: " + err.Error()
}

// disposition is what the telemetry consumer does with a batch whose landing
// failed with err (internal/bus/consumer.go, failed and nakOrTerm).
func disposition(err error) string {
	if err == nil {
		return "the batch lands and is acked"
	}
	if storage.IsRefusedWrite(err) {
		return "storage.IsRefusedWrite is true: the consumer Terms the batch at once (dropped, logged as refused)"
	}
	return "storage.IsRefusedWrite is false: the consumer Naks it with a growing delay and, after 5 deliveries, Terms it, so the whole batch, its on-time rows included, is lost"
}

func scalar[T any](ctx context.Context, conn *pgx.Conn, sql string, args ...any) (T, error) {
	var v T
	err := conn.QueryRow(ctx, sql, args...).Scan(&v)
	return v, err
}

// checksumSQL is a digest of every row in id order, to compare a table across
// a dump, a restore or a replica.
const checksumSQL = `select count(*) || ':' || md5(string_agg(id || ',' || series_id || ',' || ts || ',' || value, ';' order by id)) from metric`

// lateRow is one sample for series 1 a day into the probe span, in a
// partition or chunk that is already compressed.
var lateTS = Epoch.Add(24*time.Hour + 30*time.Minute + 123*time.Millisecond)

// RunProbes asks pg_deltax and TimescaleDB the ticket's fit questions, each by
// running the statement on a real server and recording what it answered.
func RunProbes(ctx context.Context, servers *Servers, log io.Writer) ([]Probe, error) {
	var out []Probe
	samples := Generate(probeSpec)
	for _, img := range []Image{ImageDeltax, ImageTimescaleDB} {
		sv, err := servers.Get(ctx, img)
		if err != nil {
			return out, err
		}
		tpl, err := sv.Template(ctx, probeSpec, samples)
		if err != nil {
			return out, err
		}
		e := &probeEnv{sv: sv, tpl: tpl, samples: samples, out: &out, log: log}
		if img.Key == ImageDeltax.Key {
			e.name = "pg_deltax"
			err = deltaxProbes(ctx, e)
		} else {
			e.name = "TimescaleDB"
			err = timescaleProbes(ctx, e)
		}
		if err != nil {
			return out, fmt.Errorf("%s probes: %w", e.name, err)
		}
		if err := imageProbe(ctx, e); err != nil {
			return out, err
		}
		_ = sv.DropTemplate(ctx, tpl)
	}
	return out, nil
}

// compressedDeltax is the F4 candidate built and compressed in db.
func compressedDeltax(ctx context.Context, e *probeEnv, db string) (*pgx.Conn, error) {
	conn, err := e.fresh(ctx, db)
	if err != nil {
		return nil, err
	}
	c := Deltax(probeSpec, 24*time.Hour, "")
	if err := execAll(ctx, conn, append(append(c.Setup, c.Load...), c.Compress...)); err != nil {
		conn.Close(ctx)
		return nil, err
	}
	return conn, nil
}

func deltaxProbes(ctx context.Context, e *probeEnv) error {
	// Q1: adopt the product's partitioned table?
	conn, err := e.fresh(ctx, "probe_q1")
	if err != nil {
		return err
	}
	if err := execAll(ctx, conn, append(productMetric(probeSpec), landRaw)); err != nil {
		return err
	}
	res, err := scalar[string](ctx, conn, `select deltax.deltax_create_table('metric', 'ts', interval '1 day')`)
	e.record("Q1", "deltax_create_table on the product's month-partitioned metric (migration 20261029132025)", orText(res, err))
	managed, _ := scalar[string](ctx, conn, `select string_agg(partition_name, ', ' order by range_start) from deltax.deltax_partition_info('metric')`)
	attached, _ := scalar[string](ctx, conn, `select string_agg(c.relname, ', ' order by c.relname) from pg_inherits i join pg_class c on c.oid = i.inhrelid where i.inhparent = 'metric'::regclass`)
	e.record("Q1", "the partitions deltax manages vs the partitions attached",
		fmt.Sprintf("manages [%s]; attached [%s]: the product's own month partition is attached but not managed, so it is never compressed", managed, attached))
	e.record("Q1", "deltax_drain_default_partition with the product's GENERATED ALWAYS identity, after a row lands in the default partition",
		func() string {
			if _, ierr := conn.Exec(ctx, `insert into metric (series_id, ts, value) values (1, now() - interval '20 days', 1)`); ierr != nil {
				return "could not land a default-partition row: " + ierr.Error()
			}
			_, derr := scalar[string](ctx, conn, `select deltax.deltax_drain_default_partition('metric')`)
			return sqlState(derr) + ": adoption needs the identity changed to GENERATED BY DEFAULT"
		}())
	conn.Close(ctx)
	if err := deltaxRetention(ctx, e); err != nil {
		return err
	}

	// Q2: a late row aimed at a compressed partition.
	conn, err = compressedDeltax(ctx, e, "probe_q2")
	if err != nil {
		return err
	}
	_, err = conn.Exec(ctx, `insert into metric (series_id, ts, value) values (1, $1, 42)`, lateTS)
	e.record("Q2", "INSERT one late row into a compressed day partition", sqlState(err))
	e.record("Q2", "how the classifier and the consumer answer that error", disposition(err))
	_, err = conn.Exec(ctx, `update metric set value = value where series_id = 1 and ts < $1`, lateTS)
	e.record("Q2", "UPDATE a row in a compressed partition", sqlState(err))
	_, err = conn.Exec(ctx, `delete from metric where series_id = 1 and ts < $1`, lateTS)
	e.record("Q2", "DELETE a row in a compressed partition", sqlState(err))
	conn.Close(ctx)

	// Q3: detach and drop, dump and restore, physical replication.
	conn, err = compressedDeltax(ctx, e, "probe_q3")
	if err != nil {
		return err
	}
	first, err := scalar[string](ctx, conn, `select partition_name from deltax.deltax_partition_info('metric') where is_compressed order by range_start limit 1`)
	if err != nil {
		return err
	}
	_, err = conn.Exec(ctx, `alter table public.metric detach partition `+pgx.Identifier{"public", first}.Sanitize()+` concurrently`)
	e.record("Q3", "the retention pass's DETACH PARTITION CONCURRENTLY (storage.DropMetricPartitions) on a compressed partition", sqlState(err))
	before, _ := scalar[int64](ctx, conn, `select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = '_deltax_compressed' and c.relname like $1 || '\_%'`, first)
	_, err = conn.Exec(ctx, `drop table `+pgx.Identifier{"public", first}.Sanitize())
	left, _ := scalar[int64](ctx, conn, `select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = '_deltax_compressed' and c.relname like $1 || '\_%'`, first)
	listed, _ := scalar[int64](ctx, conn, `select count(*) from deltax.deltax_partition_info('metric') where partition_name = $1`, first)
	e.record("Q3", "DROP TABLE on that compressed partition", fmt.Sprintf("%s; %d of its %d companion relations in _deltax_compressed remain, and deltax_partition_info lists it %d time(s)", sqlState(err), left, before, listed))
	conn.Close(ctx)

	conn, err = compressedDeltax(ctx, e, "probe_dump")
	if err != nil {
		return err
	}
	sum, err := scalar[string](ctx, conn, checksumSQL)
	if err != nil {
		return err
	}
	conn.Close(ctx)
	if err := dumpRestore(ctx, e, "probe_dump", "probe_restored", nil, nil, sum,
		`select count(*) filter (where is_compressed) || ' of ' || count(*) from deltax.deltax_partition_info('metric')`); err != nil {
		return err
	}
	return replicate(ctx, e, func(ctx context.Context, conn *pgx.Conn) error {
		c := Deltax(probeSpec, 24*time.Hour, "")
		return execAll(ctx, conn, append(append(c.Setup, c.Load...), c.Compress...))
	}, `select count(*) filter (where is_compressed) || ' of ' || count(*) from deltax.deltax_partition_info('metric')`)
}

// deltaxRetention is pg_deltax's own retention. It has no run-now call: its
// background worker applies deltax_set_retention, and only in the database
// pg_deltax.target_database names (postgres here), so the probe builds a
// compressed table there and waits for the worker.
func deltaxRetention(ctx context.Context, e *probeEnv) error {
	conn, err := e.sv.Connect(ctx, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	companions := `select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = '_deltax_compressed' and c.relname like 'ret\_%'`
	old := `select count(*) from deltax.deltax_partition_info('ret') where range_end <= $1`
	if err := execAll(ctx, conn, []string{
		`create extension if not exists pg_deltax`,
		`drop table if exists ret`,
		`create table ret (id bigint generated by default as identity, series_id bigint not null, ts timestamptz not null, value float8 not null)`,
		`select deltax.deltax_create_table('ret', 'ts', interval '1 day', 0)`,
		`insert into ret (series_id, ts, value) select s, t, random() from generate_series(1, 3) s,
			generate_series(timestamptz '2026-09-01', timestamptz '2026-09-03 23:59', interval '1 minute') t`,
		`select deltax.deltax_drain_default_partition('ret')`,
		`select deltax.deltax_enable_compression('ret', array['series_id'], array['ts', 'id'])`,
		`select count(*) from deltax.deltax_compress_all_partitions('ret')`,
	}); err != nil {
		return err
	}
	cut := Epoch.Add(72 * time.Hour)
	parts, _ := scalar[int64](ctx, conn, old, cut)
	before, _ := scalar[int64](ctx, conn, companions)
	if _, err := conn.Exec(ctx, `select deltax.deltax_set_retention('ret', interval '1 day')`); err != nil {
		e.record("Q1", "deltax_set_retention", sqlState(err))
		return nil
	}
	start := time.Now()
	var left int64
	for time.Since(start) < 6*time.Minute {
		if left, _ = scalar[int64](ctx, conn, old, cut); left == 0 {
			break
		}
		time.Sleep(5 * time.Second)
	}
	after, _ := scalar[int64](ctx, conn, companions)
	outcome := fmt.Sprintf("the worker dropped all %d compressed day partitions in %s and %d of their %d companion relations remain",
		parts, time.Since(start).Round(time.Second), after, before)
	if left > 0 {
		outcome = fmt.Sprintf("after %s the worker had not acted: %d of %d partitions remain", time.Since(start).Round(time.Second), left, parts)
	}
	e.record("Q1", "deltax_set_retention('1 day') on compressed day partitions in the worker's target database (no run-now call exists)", outcome)
	_, _ = conn.Exec(ctx, `select deltax.deltax_remove_retention('ret')`)
	_, _ = conn.Exec(ctx, `drop table ret`)
	return nil
}

func timescaleProbes(ctx context.Context, e *probeEnv) error {
	// Q1: adopt the product's partitioned table?
	conn, err := e.fresh(ctx, "probe_q1")
	if err != nil {
		return err
	}
	if err := execAll(ctx, conn, productMetric(probeSpec)); err != nil {
		return err
	}
	_, err = conn.Exec(ctx, `select create_hypertable('metric', by_range('ts', interval '1 day'))`)
	e.record("Q1", "create_hypertable on the product's month-partitioned metric (migration 20261029132025)", sqlState(err)+": the hypertable replaces the partitioned table and the month roll")
	conn.Close(ctx)

	compressed := func(db string) (*pgx.Conn, error) {
		conn, err := e.fresh(ctx, db)
		if err != nil {
			return nil, err
		}
		c := TimescaleDB(probeSpec, 24*time.Hour, "")
		if err := execAll(ctx, conn, append(append(c.Setup, c.Load...), c.Compress...)); err != nil {
			conn.Close(ctx)
			return nil, err
		}
		return conn, nil
	}
	conn, err = compressed("probe_ret")
	if err != nil {
		return err
	}
	n, err := scalar[int64](ctx, conn, `select count(*) from drop_chunks('metric', older_than => $1::timestamptz)`, Epoch.Add(48*time.Hour))
	left, _ := scalar[int64](ctx, conn, `select count(*) from timescaledb_information.chunks where hypertable_name = 'metric'`)
	orphans, _ := scalar[int64](ctx, conn, `select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = '_timescaledb_internal' and c.relkind = 'r'
		and c.oid not in (select format('%I.%I', schema_name, table_name)::regclass from _timescaledb_catalog.chunk where not dropped)`)
	e.record("Q1", "retention as drop_chunks(older_than) on compressed day chunks",
		fmt.Sprintf("%s: %d chunks dropped, %d remain, %d orphaned chunk tables: whole chunks go, as whole months do today", sqlState(err), n, left, orphans))
	conn.Close(ctx)

	// Q2: a late row aimed at a compressed chunk.
	conn, err = compressed("probe_q2")
	if err != nil {
		return err
	}
	_, err = conn.Exec(ctx, `insert into metric (series_id, ts, value) values (1, $1, 42)`, lateTS)
	e.record("Q2", "INSERT one late row into a compressed day chunk", sqlState(err))
	e.record("Q2", "how the classifier and the consumer answer it", disposition(err))
	got, _ := readRaw(ctx, conn, rawRead, Read{Series: 1, RawFrom: lateTS, RawTo: lateTS.Add(time.Millisecond)})
	e.record("Q2", "read the late row back", fmt.Sprintf("%d row(s) at %s, value %v", len(got), lateTS.Format(time.RFC3339Nano), pointValues(got)))
	_, err = conn.Exec(ctx, `update metric set value = value where series_id = 1 and ts < $1`, lateTS)
	e.record("Q2", "UPDATE rows in a compressed chunk", sqlState(err))
	_, err = conn.Exec(ctx, `delete from metric where series_id = 1 and ts < $1`, lateTS)
	e.record("Q2", "DELETE rows in a compressed chunk", sqlState(err))
	conn.Close(ctx)

	// Q3: detach, dump and restore, physical replication.
	conn, err = compressed("probe_q3")
	if err != nil {
		return err
	}
	chunk, err := scalar[string](ctx, conn, `select c::text from show_chunks('metric') c limit 1`)
	if err != nil {
		return err
	}
	_, err = conn.Exec(ctx, `alter table public.metric detach partition `+chunk+` concurrently`)
	e.record("Q3", "the retention pass's DETACH PARTITION CONCURRENTLY (storage.DropMetricPartitions) on a compressed chunk", sqlState(err)+": a chunk is an inheritance child, not a partition; drop_chunks is the path (Q1)")
	conn.Close(ctx)

	conn, err = compressed("probe_dump")
	if err != nil {
		return err
	}
	sum, err := scalar[string](ctx, conn, checksumSQL)
	if err != nil {
		return err
	}
	conn.Close(ctx)
	isCompressed := `select count(*) filter (where is_compressed) || ' of ' || count(*) from timescaledb_information.chunks where hypertable_name = 'metric'`
	if err := dumpRestore(ctx, e, "probe_dump", "probe_restored",
		[]string{`create extension timescaledb`, `select timescaledb_pre_restore()`},
		[]string{`select timescaledb_post_restore()`}, sum, isCompressed); err != nil {
		return err
	}
	if err := replicate(ctx, e, func(ctx context.Context, conn *pgx.Conn) error {
		c := TimescaleDB(probeSpec, 24*time.Hour, "")
		return execAll(ctx, conn, append(append(c.Setup, c.Load...), c.Compress...))
	}, isCompressed); err != nil {
		return err
	}
	return caggProbes(ctx, e, compressed)
}

func pointValues(ps []Point) []float64 {
	out := make([]float64, len(ps))
	for i, p := range ps {
		out[i] = p.Value
	}
	return out
}

// caggProbes is Q5: whether a late row reaches the continuous aggregate, by
// a manual refresh and by its refresh policy.
func caggProbes(ctx context.Context, e *probeEnv, compressed func(string) (*pgx.Conn, error)) error {
	conn, err := compressed("probe_q5")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	c := TimescaleDB(probeSpec, 24*time.Hour, "").Cagg
	if err := execAll(ctx, conn, append(c.Setup, c.Refresh...)); err != nil {
		return err
	}
	hour := lateTS.Truncate(time.Hour)
	bucket := func() string {
		s, err := scalar[string](ctx, conn, `select n || ' samples, max ' || mx from metric_hourly where series_id = 1 and bucket = $1`, hour)
		if err != nil {
			return err.Error()
		}
		return s
	}
	was := bucket()
	if _, err := conn.Exec(ctx, `insert into metric (series_id, ts, value) values (1, $1, 1e6)`, lateTS); err != nil {
		return err
	}
	stale := bucket()
	if _, err := conn.Exec(ctx, `call refresh_continuous_aggregate('metric_hourly', $1::timestamptz, $2::timestamptz)`, hour, hour.Add(time.Hour)); err != nil {
		return err
	}
	e.record("Q5", "a late row (value 1e6) into a materialized, compressed hour, then refresh_continuous_aggregate over that hour",
		fmt.Sprintf("before the row: %s; after it, before the refresh: %s (stale, the cagg is materialized-only); after the refresh: %s", was, stale, bucket()))

	if _, err := conn.Exec(ctx, `insert into metric (series_id, ts, value) values (1, $1, 2e6)`, lateTS.Add(time.Second)); err != nil {
		return err
	}
	job, err := scalar[int64](ctx, conn, `select add_continuous_aggregate_policy('metric_hourly', start_offset => interval '400 days', end_offset => interval '1 hour', schedule_interval => interval '1 hour')`)
	if err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `call run_job($1)`, job); err != nil {
		return err
	}
	e.record("Q5", "a second late row (2e6), then the refresh policy's job run (run_job), its window reaching back past the row",
		"after the policy run: "+bucket()+"; a policy whose start_offset stops short of the row leaves it out until a manual refresh")
	return nil
}

func orText(s string, err error) string {
	if err != nil {
		return sqlState(err)
	}
	return "accepted: " + s
}

// psql runs a SQL command in the container as postgres and returns its output.
func (e *probeEnv) sh(ctx context.Context, cmd string) (string, error) {
	code, r, err := e.sv.Container.Exec(ctx, []string{"sh", "-c", cmd}, tcexec.WithUser("postgres"), tcexec.Multiplexed())
	if err != nil {
		return "", err
	}
	b, _ := io.ReadAll(r)
	out := strings.TrimSpace(string(b))
	if code != 0 {
		return out, fmt.Errorf("%s: exit %d: %s", cmd, code, out)
	}
	return out, nil
}

// dumpRestore is pg_dump of db in the custom format and pg_restore into a
// new database, with the extension's own pre and post steps, holding the
// restored rows to sum.
func dumpRestore(ctx context.Context, e *probeEnv, db, into string, pre, post []string, sum, compressedSQL string) error {
	dump := "/tmp/" + db + ".dump"
	if _, err := e.sh(ctx, "pg_dump -Fc -d "+db+" -f "+dump); err != nil {
		e.record("Q3", "pg_dump -Fc", sqlState(err))
		return nil
	}
	if err := e.sv.admin(ctx, "drop database if exists "+into+" with (force)"); err != nil {
		return err
	}
	if err := e.sv.admin(ctx, "create database "+into); err != nil {
		return err
	}
	conn, err := e.sv.Connect(ctx, into)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if err := execAll(ctx, conn, pre); err != nil {
		return err
	}
	restoreOut, rerr := e.sh(ctx, "pg_restore -d "+into+" "+dump+" 2>&1")
	if err := execAll(ctx, conn, post); err != nil {
		return err
	}
	got, err := scalar[string](ctx, conn, checksumSQL)
	state, _ := scalar[string](ctx, conn, compressedSQL)
	verdict := "the restored rows match the source exactly"
	if err != nil {
		verdict = "the restored table cannot be read: " + sqlState(err)
	} else if got != sum {
		verdict = fmt.Sprintf("the restored rows differ: %s, source %s", got, sum)
	}
	restore := "pg_restore clean"
	if rerr != nil {
		restore = "pg_restore reported errors: " + firstLines(restoreOut, 3)
	}
	e.record("Q3", "pg_dump -Fc of the compressed table and pg_restore into a new database"+map[bool]string{true: " (timescaledb_pre_restore and post_restore around it)", false: ""}[len(pre) > 0],
		fmt.Sprintf("%s; %s; compressed partitions after restore: %s", restore, verdict, state))
	return nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], fmt.Sprintf("(%d more lines)", len(strings.Split(s, "\n"))-n))
	}
	return strings.Join(lines, " / ")
}

// replicate is a streaming physical standby of the server (pg_basebackup,
// the way CloudNativePG clones its replicas), with the candidate built and
// compressed on the primary after the standby is up, so the compression
// itself must cross the WAL. The standby's rows are held to the primary's.
func replicate(ctx context.Context, e *probeEnv, build func(context.Context, *pgx.Conn) error, compressedSQL string) error {
	const db = "probe_repl"
	conn, err := e.fresh(ctx, db)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	preload := ""
	for i, a := range e.sv.Image.Args {
		if i > 0 && e.sv.Image.Args[i-1] == "-c" && !strings.HasPrefix(a, "fsync") {
			preload += " -c " + a
		}
	}
	if _, err := e.sh(ctx, "pg_basebackup -h /var/run/postgresql -U postgres -D /tmp/standby -R -X stream -c fast"); err != nil {
		e.record("Q3", "pg_basebackup -R -X stream", sqlState(err))
		return nil
	}
	if _, err := e.sh(ctx, "chmod 700 /tmp/standby && pg_ctl -D /tmp/standby -l /tmp/standby.log -w start -o '-p 5433 -c unix_socket_directories=/tmp"+preload+"'"); err != nil {
		e.record("Q3", "start the standby", err.Error())
		return nil
	}
	defer func() { _, _ = e.sh(context.WithoutCancel(ctx), "pg_ctl -D /tmp/standby -m fast stop") }()
	if err := build(ctx, conn); err != nil {
		return err
	}
	sum, err := scalar[string](ctx, conn, checksumSQL)
	if err != nil {
		return err
	}
	lsn, err := scalar[string](ctx, conn, `select pg_current_wal_lsn()::text`)
	if err != nil {
		return err
	}
	q := func(sql string) (string, error) {
		return e.sh(ctx, fmt.Sprintf(`psql -h /tmp -p 5433 -d %s -Atc "%s"`, db, strings.ReplaceAll(sql, `"`, `\"`)))
	}
	deadline := time.Now().Add(time.Minute)
	for {
		caught, err := q(fmt.Sprintf("select pg_last_wal_replay_lsn() >= '%s'::pg_lsn", lsn))
		if err == nil && caught == "t" {
			break
		}
		if time.Now().After(deadline) {
			e.record("Q3", "streaming standby replay", fmt.Sprintf("did not reach the primary's WAL in a minute: %s %v", caught, err))
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	got, gerr := q(checksumSQL)
	state, _ := q(compressedSQL)
	verdict := "the standby's rows match the primary's exactly"
	if gerr != nil {
		verdict = "the standby cannot read the table: " + firstLines(gerr.Error(), 2)
	} else if got != sum {
		verdict = fmt.Sprintf("the standby's rows differ: %s, primary %s", got, sum)
	}
	e.record("Q3", "a streaming physical standby (pg_basebackup -R -X stream), the table built and compressed on the primary after it started",
		fmt.Sprintf("%s; compressed partitions on the standby: %s", verdict, state))

	// A failover promotes the standby (CloudNativePG's switchover does the
	// same), so the replica's data is also held to the primary's once promoted.
	if _, err := e.sh(ctx, "pg_ctl -D /tmp/standby -w promote"); err != nil {
		e.record("Q3", "promote the standby (pg_ctl promote)", err.Error())
		return nil
	}
	got, gerr = q(checksumSQL)
	verdict = "the promoted standby's rows match the primary's exactly"
	if gerr != nil {
		verdict = "the promoted standby cannot read the table: " + firstLines(gerr.Error(), 2)
	} else if got != sum {
		verdict = fmt.Sprintf("the promoted standby's rows differ: %s, primary %s", got, sum)
	}
	e.record("Q3", "promote that standby (pg_ctl promote), as a failover does, and read the table", verdict)
	return nil
}

// imageProbe is Q4's measurable half: the image's size over the plain
// postgres:18 it is built on, the extension's shared objects, and its
// licenses in place.
func imageProbe(ctx context.Context, e *probeEnv) error {
	size := func(ref string) (int64, error) {
		out, err := exec.CommandContext(ctx, "docker", "image", "inspect", "-f", "{{.Size}}", ref).Output()
		if err != nil {
			return 0, fmt.Errorf("docker image inspect %s: %w", ref, err)
		}
		var n int64
		_, err = fmt.Sscan(strings.TrimSpace(string(out)), &n)
		return n, err
	}
	base, err := size(ImagePlain.Ref)
	if err != nil {
		return err
	}
	img, err := size(e.sv.Image.Ref)
	if err != nil {
		return err
	}
	glob := map[string]string{"pg_deltax": "pg_deltax*.so", "timescaledb": "timescaledb*.so"}[e.sv.Image.Extension]
	so, err := e.sh(ctx, "cd /usr/lib/postgresql/18/lib && ls "+glob+" | wc -l && du -cb "+glob+" | tail -1 | cut -f1")
	if err != nil {
		return err
	}
	parts := strings.Fields(so)
	var lics []string
	for _, f := range map[string][]string{
		"pg_deltax":   {"/usr/share/doc/pg-deltax-pg18/LICENSE"},
		"timescaledb": {"/usr/share/doc/timescaledb/LICENSE", "/usr/share/doc/timescaledb/LICENSE-TIMESCALE"},
	}[e.sv.Image.Extension] {
		if _, err := e.sh(ctx, "test -s "+f); err == nil {
			lics = append(lics, f)
		}
	}
	lic := strings.Join(lics, ", ")
	if lic == "" {
		lic = "none found"
	}
	e.record("Q4", "docker image inspect, against postgres:18, and the extension's shared objects",
		fmt.Sprintf("%s is %s, %s over postgres:18 (%s); %s shared objects totalling %s; licenses at %s",
			e.sv.Image.Ref, mb(img), mb(img-base), mb(base), parts[0], mb(atoi(parts[1])), strings.TrimSpace(lic)))
	return nil
}

func atoi(s string) int64 {
	var n int64
	_, _ = fmt.Sscan(s, &n)
	return n
}

func mb(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/1e6) }

// ProbeTable is the probes as markdown, one row a step.
func ProbeTable(ps []Probe) string {
	var b strings.Builder
	b.WriteString("| Q | extension | what ran | what the server answered |\n|---|---|---|---|\n")
	for _, p := range ps {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", p.Question, p.Candidate, cell(p.Step), cell(p.Outcome))
	}
	return b.String()
}

func cell(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ") }
