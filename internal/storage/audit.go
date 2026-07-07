package storage

import (
	"context"
	"fmt"
)

// AuditEntry is one row of the audit trail, with the actor (and, for an
// impersonated action, the real actor behind it) resolved to a human username
// where possible so the read surface is legible without an N+1 lookup.
type AuditEntry struct {
	ID            string
	TS            string // RFC3339
	ActorID       string // empty for a system/bootstrap write
	ActorName     string // human username if the actor is a human, else empty
	RealActorID   string // set when the action was taken while impersonating
	RealActorName string
	Verb          string
	Resource      string
	ResourceID    string
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

// ListAuditLog returns recent audit rows, newest first, resolving the actor and
// real-actor human usernames. It is the read side of the audit trail; writes go
// through writeAuditRes (estate mutations, in the caller's tx) and WriteAuthEvent
// (auth events, no tx).
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
		       coalesce(a.actor_principal_id::text, ''), coalesce(ah.username, ''),
		       coalesce(a.real_actor_principal_id::text, ''), coalesce(rh.username, ''),
		       a.verb, a.resource, coalesce(a.resource_id, '')
		from audit_log a
		left join human ah on ah.principal_id = a.actor_principal_id
		left join human rh on rh.principal_id = a.real_actor_principal_id
		where ($2 = '' or a.resource = $2)
		  and ($3 = '' or a.verb = $3)
		  and ($4 = '' or a.ts < $4::timestamptz)
		order by a.ts desc, a.id desc
		limit $1`,
		limit, f.Resource, f.Verb, f.Before)
	if err != nil {
		return nil, fmt.Errorf("storage: list audit log: %w", err)
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.TS, &e.ActorID, &e.ActorName, &e.RealActorID, &e.RealActorName, &e.Verb, &e.Resource, &e.ResourceID); err != nil {
			return nil, fmt.Errorf("storage: scan audit row: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// WriteAuthEvent records an authentication event (login, logout) in the audit
// trail. Unlike writeAuditRes, it takes no transaction: login and logout are
// read/no-tx paths, so the emit is a standalone autocommit insert. It still
// records the real actor from context, for consistency, though auth events are
// not impersonated.
func (p *PG) WriteAuthEvent(ctx context.Context, actorID, verb string) error {
	if _, err := p.pool.Exec(ctx, `
		insert into audit_log (actor_principal_id, real_actor_principal_id, verb, resource)
		values ($1, $2, $3, 'auth')`,
		nullize(actorID), nullize(realActorFrom(ctx)), verb); err != nil {
		return fmt.Errorf("storage: write auth event: %w", err)
	}
	return nil
}
