package labfootprint

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/client"
)

// The retention drain probe plants a backlog of aged property rows, restarts
// the server so its retention pass runs at start, and reads how fast the pass
// clears them (#1556). It measures the property lane: that lane's retention
// deletes rows in chunks, so its rate is a drain rate; the metric lane's
// retention mostly drops whole daily partitions, which has no rate to read.
//
// Every aged row is paired with a fresh row of its own series (the instance
// is per pair), because the prune spares a series' latest row: the aged row
// is only deletable when a newer one exists.
const (
	drainInstancePrefix = "drain-probe-"
	// drainAgeDays is past the default 90 day property window.
	drainAgeDays = 120
	// drainWatchedDays is inside that age and outside the window, so the
	// count of rows older than it is the backlog's remaining rows.
	drainWatchedDays = 100
	drainEvery       = 500 * time.Millisecond
)

// plantAgedSQL and plantFreshSQL insert n aged rows, then a fresh row of
// each one's series, on one component and one property type with no window
// of its own. Two statements, so every fresh row's id is higher than its aged
// row's, which the prune's "a newer row exists" test needs.
func plantAgedSQL(n int64) string {
	return plantSQL(n, fmt.Sprintf("now() - interval '%d days' - i * interval '1 millisecond'", drainAgeDays))
}

func plantFreshSQL(n int64) string { return plantSQL(n, "now()") }

func plantSQL(n int64, ts string) string {
	return fmt.Sprintf(`insert into property (owner_kind, component_id, property_type_id, instance, provenance, value, ts)
		select 'component', (select id from component order by id limit 1),
		       (select id from property_type where retention_days is null order by id limit 1),
		       '%s' || i, 'observed', '1'::jsonb, %s
		from generate_series(1, %d) i`, drainInstancePrefix, ts, n)
}

func remainingBacklogSQL() string {
	return fmt.Sprintf(`select count(*) from property where instance like '%s%%' and ts < now() - interval '%d days'`,
		drainInstancePrefix, drainWatchedDays)
}

func clearBacklogSQL() string {
	return fmt.Sprintf(`delete from property where instance like '%s%%'`, drainInstancePrefix)
}

func (h *harness) drainProbe(ctx context.Context, server string, cfg ScaleConfig) (Drain, error) {
	h.say("planting %d aged property rows for the retention drain probe", cfg.Backlog)
	if _, err := h.psql(ctx, clearBacklogSQL()); err != nil {
		return Drain{}, fmt.Errorf("clear an earlier probe: %w", err)
	}
	for _, q := range []string{plantAgedSQL(cfg.Backlog), plantFreshSQL(cfg.Backlog)} {
		if _, err := h.psql(ctx, q); err != nil {
			return Drain{}, fmt.Errorf("plant the backlog: %w", err)
		}
	}
	// A fresh context for the cleanup: a cancelled run still removes its rows.
	defer func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		if _, err := h.psql(cctx, clearBacklogSQL()); err != nil {
			h.say("could not remove the probe's rows: %v", err)
		}
	}()
	planted, err := h.remaining(ctx)
	if err != nil {
		return Drain{}, err
	}
	if planted != cfg.Backlog {
		return Drain{}, fmt.Errorf("planted %d aged rows, want %d: is there a component and a property type without a window?", planted, cfg.Backlog)
	}

	h.say("restarting the server so its retention pass runs")
	started := time.Now()
	if _, err := h.dc.ContainerRestart(ctx, server, client.ContainerRestartOptions{}); err != nil {
		return Drain{}, fmt.Errorf("restart the server: %w", err)
	}
	var samples []DrainSample
	deadline := started.Add(cfg.DrainTimeout)
	for {
		left, err := h.remaining(ctx)
		if err != nil {
			// The server's restart does not stop the database; an error
			// here is a real one.
			return Drain{}, err
		}
		samples = append(samples, DrainSample{At: time.Now(), Remaining: left})
		if left == 0 || time.Now().After(deadline) {
			break
		}
		if err := sleep(ctx, drainEvery); err != nil {
			return Drain{}, err
		}
	}
	d := DrainOf(cfg.Backlog, samples)
	if d.Drained == 0 {
		// Say what the server's retention pass logged, so a probe that saw
		// nothing delete is diagnosable from its own output.
		if log, err := h.serverLog(ctx, server, started); err == nil {
			for _, line := range strings.Split(log, "\n") {
				if strings.Contains(line, "retention") {
					h.say("  server: %s", strings.TrimSpace(line))
				}
			}
		}
	}
	return d, nil
}

func (h *harness) remaining(ctx context.Context) (int64, error) {
	out, err := h.psql(ctx, remainingBacklogSQL())
	if err != nil {
		return 0, fmt.Errorf("read the backlog: %w", err)
	}
	n, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0, errors.New("read the backlog: not a count")
	}
	return n, nil
}
