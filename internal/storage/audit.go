package storage

import (
	"context"
	"fmt"
)

// AuditEntry is one row of the audit trail, with the actor (and, for an
// impersonated action, the real actor behind it) resolved to its identifier so
// the read surface is legible without an N+1 lookup.
type AuditEntry struct {
	ID            string
	TS            string // RFC3339
	ActorID       string // empty for a system/bootstrap write
	ActorName     string // the actor's identifier: a human's username, a service account's name, a node's name
	RealActorID   string // set when the action was taken while impersonating
	RealActorName string
	Verb          string
	Resource      string
	ResourceID    string
	// Old and New are the row images writeAuditRes recorded (raw JSON), the
	// "to what?" half of the trail. Nil when the write had no image on that
	// side: a create has no old, a delete no new. Callers pass them through
	// verbatim; the write side owns redaction (sealed material never lands
	// here, see auditSecret and the credential writes).
	Old []byte
	New []byte
}

// AuditFilter bounds a ListAuditLog read. Limit caps the rows (a sane default is
// applied when zero). Resource and Verb, when set, filter to that kind of event.
// Before, when set (RFC3339), pages backward: only rows strictly older than it.
type AuditFilter struct {
	Limit    int
	Resource string
	Verb     string
	Before   string
}

const auditDefaultLimit = 100
const auditMaxLimit = 500

// ListAuditLog returns recent audit rows, newest first, resolving each actor to
// its identifier. It is the read side of the audit trail; writes go through
// writeAuditRes (fleet mutations, in the caller's tx) and WriteAuthEvent (auth
// events, no tx).
//
// Live first, then the snapshot the write denormalized. The live resolution is
// the same one every other surface uses (principalIdent), so a principal that
// still exists reads as whatever it is called NOW, and a purged one reads as
// whatever it was called at the time. This used to name `human.username` by
// hand, which resolved a human actor live and a service actor only from the
// snapshot: a fourth statement of a policy stated three times too often, and
// asymmetric between two kinds for no reason anybody wrote down.
func (p *PG) ListAuditLog(ctx context.Context, f AuditFilter) ([]AuditEntry, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = auditDefaultLimit
	}
	if limit > auditMaxLimit {
		limit = auditMaxLimit
	}
	rows, err := p.pool.Query(ctx, `
		select a.id, to_char(a.ts, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		       coalesce(a.actor_principal_id::text, ''),
		       `+principalIdentJoinedCols("ac")+`, coalesce(a.actor_username, ''),
		       coalesce(a.real_actor_principal_id::text, ''),
		       `+principalIdentJoinedCols("ra")+`, coalesce(a.real_actor_username, ''),
		       a.verb, a.resource, coalesce(a.resource_id, ''), a.old, a.new
		from audit_log a`+
		principalIdentJoins("ac", "a.actor_principal_id")+
		principalIdentJoins("ra", "a.real_actor_principal_id")+`
		where ($2 = '' or a.resource = $2)
		  and ($3 = '' or a.verb = $3)
		  and ($4 = '' or a.ts < $4::timestamptz)
		-- id (uuidv7) is the true write sequence; ts is a display fact. The
		-- two can invert across transactions (a tx stamps its audit row
		-- before slower work commits after a later tx's row), and ordering
		-- by ts made the page's row order flap between two reads of the
		-- same trail (#780's audit half).
		order by a.id desc
		limit $1`,
		limit, f.Resource, f.Verb, f.Before)
	if err != nil {
		return nil, fmt.Errorf("storage: list audit log: %w", err)
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var actorUser, actorSvc, actorNode, actorSnap *string
		var realUser, realSvc, realNode, realSnap *string
		if err := rows.Scan(&e.ID, &e.TS,
			&e.ActorID, &actorUser, &actorSvc, &actorNode, &actorSnap,
			&e.RealActorID, &realUser, &realSvc, &realNode, &realSnap,
			&e.Verb, &e.Resource, &e.ResourceID, &e.Old, &e.New); err != nil {
			return nil, fmt.Errorf("storage: scan audit row: %w", err)
		}
		e.ActorName = liveOrSnapshot(principalIdent(actorUser, actorSvc, actorNode), actorSnap)
		e.RealActorName = liveOrSnapshot(principalIdent(realUser, realSvc, realNode), realSnap)
		out = append(out, e)
	}
	return out, rows.Err()
}

// liveOrSnapshot prefers the identifier the principal has NOW and falls back to
// the one the write denormalized onto the row. The snapshot is what makes the
// trail survive a purge (ADR-0016), and it is the only thing left to read once
// the foreign key has gone null.
func liveOrSnapshot(live string, snapshot *string) string {
	if live != "" {
		return live
	}
	if snapshot != nil {
		return *snapshot
	}
	return ""
}

// WriteAuthEvent records an authentication event (login, logout) in the audit
// trail. Unlike writeAuditRes, it takes no transaction: login and logout are
// read/no-tx paths, so the emit is a standalone autocommit insert. It still
// records the real actor from context, for consistency, though auth events are
// not impersonated.
func (p *PG) WriteAuthEvent(ctx context.Context, actorID, verb string) error {
	if _, err := p.pool.Exec(ctx, `
		insert into audit_log (actor_principal_id, real_actor_principal_id, actor_username, real_actor_username, verb, resource)
		values ($1, $2, `+principalIdentSQL("$1")+`, `+principalIdentSQL("$2")+`, $3, 'auth')`,
		nullize(actorID), nullize(realActorFrom(ctx)), verb); err != nil {
		return fmt.Errorf("storage: write auth event: %w", err)
	}
	return nil
}
