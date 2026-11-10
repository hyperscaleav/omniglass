package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// CommandQueue is one node's command queue at a health read (#1626). The
// gauges (Pending, Oldest, Stuck) are the queue now; the counts
// (Redelivered, ExpiredUndelivered, ExpiredUnexecuted) are the window since
// the previous read.
type CommandQueue struct {
	Node        string
	ComponentID string
	// Pending is every live command the node actuates: due on its next pull,
	// or taken and awaiting its report.
	Pending int
	// Oldest is when the oldest pending command was issued; nil when none,
	// which CommandQueueMetrics writes as an age of zero.
	Oldest *time.Time
	// Stuck is the pending commands taken and silent past redeliverAfter.
	Stuck int
	// Redelivered is the commands taken again in the window shifted back by
	// redeliveryCountLag, executed since or not.
	Redelivered int
	// ExpiredUndelivered and ExpiredUnexecuted are the commands that crossed
	// deliveryTTL unexecuted in the window, never taken and taken but never
	// reported.
	ExpiredUndelivered int
	ExpiredUnexecuted  int
}

// CommandQueueRead is the fleet's command queues at one read: every node
// with a platform component, an idle one at zero, and the database's time,
// which the next read's window starts from.
type CommandQueueRead struct {
	Now   time.Time
	Nodes []CommandQueue
}

// redeliveryCountLag shifts the redelivery window back from the read. A
// redelivery's stamp is its dispatch transaction's start, so a read that runs
// before that transaction commits cannot see it, and a window ending at the
// read's own time would have moved past the stamp by the next read: the
// redelivery would fall between two windows. The shifted windows still tile
// (each starts where the last ended), and a dispatch that commits within the
// lag of its stamp is counted exactly once, up to the lag late.
const redeliveryCountLag = "5 seconds"

// commandHealthSQL is the fleet's queue health in one statement ($1 the
// window's start, null for none). The candidates are the pull's own resolution
// (commandCandidates), widened to the commands live at the window's start so
// a command that expired in the window is still resolved to its node. Each
// command is attributed to the node its chosen endpoint is placed on.
// Redeliveries are read apart: a command redelivered and then executed has
// left the candidates, and the redelivered rows are few
// (command_redelivered_idx). Their window is the same one shifted back by
// redeliveryCountLag.
var commandHealthSQL = `
	with candidate as (` + commandCandidates("coalesce($1::timestamptz, now()) - interval '"+deliveryTTL+"'") + `),
	queue as (
		select k.node_name,
			count(*) filter (where k.ts > now() - interval '` + deliveryTTL + `') as pending,
			min(k.ts) filter (where k.ts > now() - interval '` + deliveryTTL + `') as oldest,
			count(*) filter (where k.ts > now() - interval '` + deliveryTTL + `'
				and k.dispatched_at < now() - interval '` + redeliverAfter + `') as stuck,
			count(*) filter (where k.ts <= now() - interval '` + deliveryTTL + `' and k.first_dispatched_at is null) as expired_undelivered,
			count(*) filter (where k.ts <= now() - interval '` + deliveryTTL + `' and k.first_dispatched_at is not null) as expired_unexecuted
		from candidate k
		group by k.node_name
	),
	redelivered as (
		select i.node_name, count(*) as n
		from command c join endpoint i on i.id = c.dispatched_endpoint_id
		where c.delivery_count > 1
		  and c.dispatched_at > coalesce($1::timestamptz, now()) - interval '` + redeliveryCountLag + `'
		  and c.dispatched_at <= now() - interval '` + redeliveryCountLag + `'
		group by i.node_name
	)
	select now(), n.name, n.platform_component_id::text,
		coalesce(q.pending, 0), q.oldest, coalesce(q.stuck, 0), coalesce(r.n, 0),
		coalesce(q.expired_undelivered, 0), coalesce(q.expired_unexecuted, 0)
	from node n
	left join queue q on q.node_name = n.principal_id
	left join redelivered r on r.node_name = n.principal_id
	where n.platform_component_id is not null
	order by n.name`

// CommandQueueHealth reads every node's command queue (#1626) for the window
// since the previous read's Now, in one statement for the whole fleet. A zero
// since (the first read) reads the gauges over an empty window.
func (p *PG) CommandQueueHealth(ctx context.Context, since time.Time) (CommandQueueRead, error) {
	var out CommandQueueRead
	var start *time.Time
	if !since.IsZero() {
		start = &since
	}
	rows, err := p.pool.Query(ctx, commandHealthSQL, start)
	if err != nil {
		return out, fmt.Errorf("storage: command queue health: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var q CommandQueue
		if err := rows.Scan(&out.Now, &q.Node, &q.ComponentID, &q.Pending, &q.Oldest, &q.Stuck, &q.Redelivered,
			&q.ExpiredUndelivered, &q.ExpiredUnexecuted); err != nil {
			return out, fmt.Errorf("storage: scan command queue health: %w", err)
		}
		out.Nodes = append(out.Nodes, q)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("storage: command queue health: %w", err)
	}
	if out.Now.IsZero() {
		if err := p.pool.QueryRow(ctx, `select now()`).Scan(&out.Now); err != nil {
			return out, fmt.Errorf("storage: command queue health time: %w", err)
		}
	}
	return out, nil
}

// CommandQueueMetrics is a health read as the platform metrics it lands as,
// each at the read's time on the node's platform component. The gauges are
// written every read, an idle queue's pending age as zero: an age written
// only while something waits would leave a drained queue's last age standing
// as its latest sample, and a rule on it would never clear. The window
// counts are written every read with a window, a quiet window's as zero, for
// the same reason: a count written only when nonzero would leave the last
// event's count standing as the latest value long after it. A read with no
// window (a zero since) writes none: a first read cannot know what crossed
// before it.
func CommandQueueMetrics(read CommandQueueRead, since time.Time) []PlatformMetric {
	var out []PlatformMetric
	for _, q := range read.Nodes {
		add := func(key, instance string, v float64) {
			out = append(out, PlatformMetric{ComponentID: q.ComponentID, Key: key, Instance: instance, Value: v, TS: read.Now})
		}
		add(writerclass.CommandPendingCount, "", float64(q.Pending))
		add(writerclass.CommandStuckCount, "", float64(q.Stuck))
		age := 0.0
		if q.Oldest != nil {
			age = max(read.Now.Sub(*q.Oldest).Seconds(), 0)
		}
		add(writerclass.CommandPendingAge, "", age)
		if since.IsZero() {
			continue
		}
		for _, c := range []struct {
			key, instance string
			n             int
		}{
			{writerclass.CommandRedeliveryCount, "", q.Redelivered},
			{writerclass.CommandExpiredCount, writerclass.CommandExpiredUndelivered, q.ExpiredUndelivered},
			{writerclass.CommandExpiredCount, writerclass.CommandExpiredUnexecuted, q.ExpiredUnexecuted},
		} {
			add(c.key, c.instance, float64(c.n))
		}
	}
	return out
}
