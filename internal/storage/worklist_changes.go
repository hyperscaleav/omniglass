package storage

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// A write that changes what a node's worklist holds announces the node on
// commit (#1945, ADR-1945; every writer beyond the endpoint's own, #1997), so
// the bus can nudge it to pull now rather than on its next scheduled pull. The announcement is a Postgres NOTIFY issued in the
// writing transaction: Postgres delivers it only if that transaction commits,
// once per node however many rows the write touched. Nothing depends on it
// arriving: a lost one costs the node only the wait until its scheduled pull.

// worklistChangedChannel is the NOTIFY channel; each payload is a node name.
const worklistChangedChannel = "og_worklist_changed"

// WorklistListenApp is the application_name of the connection WorklistChanges
// listens on, so an operator (or a test) can find it in pg_stat_activity.
const WorklistListenApp = "omniglass-worklist-listen"

// The listener's reconnect backoff after its connection drops.
const (
	listenBackoffBase = 100 * time.Millisecond
	listenBackoffCap  = 5 * time.Second
)

// notifyWorklistChanged announces the node each endpoint is placed on, as of
// this point in tx. A write that moves an endpoint off a node, or deletes it,
// calls it before the change as well as after, so both nodes hear.
func notifyWorklistChanged(ctx context.Context, tx pgx.Tx, endpointIDs ...string) error {
	return announceNodesOf(ctx, tx, `select node_name from endpoint where id = any($2)`, endpointIDs)
}

// announceNodesOf announces each node whose principal id endpointNodesSQL
// selects (an endpoint.node_name column, as of this point in tx). The query's
// own arguments start at $2: $1 is the channel. It is the one way a writer
// names the nodes it touched, so each writer states only which ones.
func announceNodesOf(ctx context.Context, tx pgx.Tx, endpointNodesSQL string, args ...any) error {
	if _, err := tx.Exec(ctx, `
		select pg_notify($1, n.name) from node n
		where n.principal_id in (`+endpointNodesSQL+`)`,
		append([]any{worklistChangedChannel}, args...)...); err != nil {
		return fmt.Errorf("storage: announce a worklist change: %w", err)
	}
	return nil
}

// announceEveryNode announces every node, for a write every worklist carries
// (the platform report window).
func announceEveryNode(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `select pg_notify($1, name) from node`, worklistChangedChannel); err != nil {
		return fmt.Errorf("storage: announce a worklist change: %w", err)
	}
	return nil
}

// secretUserNodesSQL selects the nodes of the endpoints that resolve secret
// $2: an endpoint whose inputs name the secret and whose component the
// secret's owner covers (the platform, the component or an ancestor, or a
// location in the component's chain). It is a superset of the endpoints the
// secret actually wins for, which costs a spare pull at most; a shadowed
// endpoint's worklist does not change, but an announcement it did not need is
// harmless where a missed one is a wait.
const secretUserNodesSQL = `
	with recursive s as (
		select name, owner_kind, component_id, location_id from secret where id = $2
	), comps(id) as (
		select component_id from s where owner_kind = 'component'
		union select c.id from component c join comps on c.parent_id = comps.id
	), locs(id) as (
		select location_id from s where owner_kind = 'location'
		union select l.id from location l join locs on l.parent_id = locs.id
	)
	select e.node_name from endpoint e, s
	where exists (select 1 from jsonb_each_text(e.inputs) i where i.value = s.name)
	  and (s.owner_kind = 'platform'
	       or e.component in (select id from comps)
	       or e.component in (select c.id from component c where c.location_id in (select id from locs)))`

// pathEdgeNodesSQL selects the nodes whose worklist carries the path edge
// from component $2 to upstream $3: those where both have an in-band endpoint,
// the join NodeWorklist's path upstreams make.
const pathEdgeNodesSQL = `
	select i.node_name from endpoint i
	join endpoint u on u.node_name = i.node_name and u.component = $3 and not u.out_of_band
	where i.component = $2 and not i.out_of_band`

// pathDependentNodesSQL selects the nodes whose worklist carries component $2
// as a path upstream, by name: a rename changes each of them.
const pathDependentNodesSQL = `
	select i.node_name from component_dependency d
	join endpoint i on i.component = d.component_id and not i.out_of_band
	join endpoint u on u.node_name = i.node_name and u.component = d.upstream_id and not u.out_of_band
	where d.upstream_id = $2 and d.kind = 'path'`

// componentNodesSQL selects the nodes of component $2's own endpoints: a
// relocation changes the location secrets only they resolve.
const componentNodesSQL = `select node_name from endpoint where component = $2`

// subtreeComponentNodesSQL selects the nodes of every endpoint on component
// $2 or a descendant: a reparent changes which component secrets their
// inputs resolve.
const subtreeComponentNodesSQL = `
	with recursive sub(id) as (
		select $2::uuid union select c.id from component c join sub on c.parent_id = sub.id
	)
	select e.node_name from endpoint e where e.component in (select id from sub)`

// subtreeLocationNodesSQL selects the nodes of every endpoint on a component
// placed in location $2 or below it: a location move changes the chain their
// location secrets resolve along.
const subtreeLocationNodesSQL = `
	with recursive sub(id) as (
		select $2::uuid union select l.id from location l join sub on l.parent_id = sub.id
	)
	select e.node_name from endpoint e join component c on c.id = e.component
	where c.location_id in (select id from sub)`

// WorklistChanges listens for worklist changes and sends each announced node
// name on the returned channel until ctx ends, when it closes. When the first
// connect succeeds it returns with the listen in place, so a change committed
// after it returns arrives. The listen holds one connection of its own,
// outside the pool. When that connection cannot be opened, or drops, it is
// retried on backoff, and the changes committed while it was down are not
// replayed: a nudge only shortens the wait, so the listener never stops the
// server, and a node it misses pulls on its schedule.
func (p *PG) WorklistChanges(ctx context.Context) (<-chan string, error) {
	conn, err := p.listenWorklist(ctx)
	if err != nil {
		slog.Warn("worklist change listener could not connect, retrying", "facility", "collection",
			"retry_in", listenBackoffBase.String(), "error", err.Error())
	}
	out := make(chan string, 64)
	go func() {
		defer close(out)
		backoff := listenBackoffBase
		for {
			if conn != nil {
				up := time.Now()
				err := forwardChanges(ctx, conn, out)
				cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				_ = conn.Close(cctx)
				cancel()
				conn = nil
				if ctx.Err() != nil {
					return
				}
				// A connection that held resets the backoff; one dropped as
				// soon as it listened (a pooler that refuses LISTEN, a
				// reaper) keeps backing off, so the retry does not spin.
				if time.Since(up) >= listenBackoffCap {
					backoff = listenBackoffBase
				}
				slog.Warn("worklist change listener dropped, reconnecting", "facility", "collection",
					"retry_in", backoff.String(), "error", err.Error())
			}
			t := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			backoff = min(backoff*2, listenBackoffCap)
			if conn, err = p.listenWorklist(ctx); err != nil {
				slog.Warn("worklist change listener could not reconnect", "facility", "collection",
					"retry_in", backoff.String(), "error", err.Error())
			}
		}
	}()
	return out, nil
}

// listenWorklist opens the listen connection and LISTENs on it.
func (p *PG) listenWorklist(ctx context.Context) (*pgx.Conn, error) {
	cfg := p.pool.Config().ConnConfig.Copy()
	cfg.RuntimeParams["application_name"] = WorklistListenApp
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("storage: connect the worklist change listener: %w", err)
	}
	if _, err := conn.Exec(ctx, "listen "+worklistChangedChannel); err != nil {
		_ = conn.Close(ctx)
		return nil, fmt.Errorf("storage: listen for worklist changes: %w", err)
	}
	return conn, nil
}

// forwardChanges sends each notification's node name to out until the
// connection fails or ctx ends, returning why.
func forwardChanges(ctx context.Context, conn *pgx.Conn, out chan<- string) error {
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if n.Payload == "" {
			continue // a bare NOTIFY names no node; nothing to nudge
		}
		select {
		case out <- n.Payload:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
