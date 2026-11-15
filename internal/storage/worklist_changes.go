package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// A write that changes what a node's worklist holds announces the node on
// commit (#1945, ADR-1945), so the bus can nudge it to pull now rather than on
// its next scheduled pull. The announcement is a Postgres NOTIFY issued in the
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
	if _, err := tx.Exec(ctx, `
		select pg_notify($1, n.name) from node n
		where n.principal_id in (select node_name from endpoint where id = any($2))`,
		worklistChangedChannel, endpointIDs); err != nil {
		return fmt.Errorf("storage: announce a worklist change: %w", err)
	}
	return nil
}

// WorklistChanges listens for worklist changes and sends each announced node
// name on the returned channel until ctx ends, when it closes. It returns once
// the listen is in place, so a change committed after it returns arrives. The
// listen holds one connection of its own, outside the pool; when that
// connection drops it reconnects on backoff, and the changes committed while
// it was down are not replayed.
func (p *PG) WorklistChanges(ctx context.Context) (<-chan string, error) {
	conn, err := p.listenWorklist(ctx)
	if err != nil {
		return nil, err
	}
	out := make(chan string, 64)
	go func() {
		defer close(out)
		backoff := listenBackoffBase
		for {
			if conn != nil {
				err := forwardChanges(ctx, conn, out)
				cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				_ = conn.Close(cctx)
				cancel()
				conn = nil
				if ctx.Err() != nil {
					return
				}
				slog.Warn("worklist change listener dropped, reconnecting", "facility", "collection", "error", err.Error())
			}
			t := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			if conn, err = p.listenWorklist(ctx); err != nil {
				backoff = min(backoff*2, listenBackoffCap)
				slog.Warn("worklist change listener could not reconnect", "facility", "collection",
					"retry_in", backoff.String(), "error", err.Error())
				continue
			}
			backoff = listenBackoffBase
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
			return errors.New("storage: an empty worklist change")
		}
		select {
		case out <- n.Payload:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
