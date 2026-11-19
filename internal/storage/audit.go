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
	ActorName     string // the actor's identifier: a person's username, a machine user's name, an outpost's name
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
	// Channel is how the action arrived (#1162): "mcp" for an MCP tool call,
	// empty for a direct API call. Client is the name and version the MCP client
	// declared about itself, as asserted; empty when there is none.
	Channel string
	Client  string
	// Agent names the agent that took the action on the actor's behalf, with
	// an agent token the actor minted (#1179); empty when the actor acted
	// directly.
	Agent string
}

// AuditFilter bounds a ListAuditLog read. Limit caps the rows (a sane default is
// applied when zero). Resource and Verb, when set, filter to that kind of event.
// Before, when set (RFC3339), pages backward: only rows strictly older than it.
type AuditFilter struct {
	Limit    int
	Resource string
	Verb     string
	Before   string
	// Agent, when set, keeps only the rows that agent took (#1179).
	Agent string
	// Channel, when set, keeps only the rows that arrived through it (#1178):
	// AuditChannelDirect for rows with no channel, else a channel's name.
	Channel string
	// Client, when set, keeps only the rows whose declared client contains it,
	// case-insensitively, so one term matches every version of a client.
	Client string
}

// AuditChannelDirect is the channel filter value for a row with no channel: a
// direct API call. It is a filter term only, never stored.
const AuditChannelDirect = "direct"

const auditDefaultLimit = 100
const auditMaxLimit = 500

// ListAuditLog returns recent audit rows, newest first, resolving each actor to
// its identifier. It is the read side of the audit trail; writes go through
// writeAuditRes (fleet mutations, in the caller's tx) and WriteAuthEvent (auth
// events, no tx).
//
// Live first, then the snapshot the write denormalized. The live resolution is
// the same one every other surface uses (userIdent), so a user that
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
		       coalesce(a.actor_user_id::text, ''),
		       `+userIdentJoinedCols("ac")+`, coalesce(a.actor_username, ''),
		       coalesce(a.real_actor_user_id::text, ''),
		       `+userIdentJoinedCols("ra")+`, coalesce(a.real_actor_username, ''),
		       a.verb, a.resource, coalesce(a.resource_id, ''), a.old, a.new,
		       coalesce(a.channel, ''), coalesce(a.client, ''), coalesce(a.agent, '')
		from audit_log a`+
		userIdentJoins("ac", "a.actor_user_id")+
		userIdentJoins("ra", "a.real_actor_user_id")+`
		where ($2 = '' or a.resource = $2)
		  and ($3 = '' or a.verb = $3)
		  and ($4 = '' or a.ts < $4::timestamptz)
		  and ($5 = '' or a.agent = $5)
		  and ($6 = '' or ($6 = '`+AuditChannelDirect+`' and a.channel is null) or a.channel = $6)
		  and ($7 = '' or strpos(lower(a.client), lower($7)) > 0)
		-- seq (the DB-assigned identity) is the true write sequence; ts is a
		-- display fact, and the uuidv7 id is a clock in disguise. Both stamp
		-- kinds invert across transactions (a slow tx commits after a later
		-- one, or the wall clock itself steps mid-run, WSL2 NTP corrections
		-- being the reproduced case), and each in turn made the page's row
		-- order flap between two reads of the same trail (#780).
		order by a.seq desc
		limit $1`,
		limit, f.Resource, f.Verb, f.Before, f.Agent, f.Channel, f.Client)
	if err != nil {
		return nil, fmt.Errorf("storage: list audit log: %w", err)
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var actorUser, actorSvc, actorOutpost, actorSnap *string
		var realUser, realSvc, realOutpost, realSnap *string
		if err := rows.Scan(&e.ID, &e.TS,
			&e.ActorID, &actorUser, &actorSvc, &actorOutpost, &actorSnap,
			&e.RealActorID, &realUser, &realSvc, &realOutpost, &realSnap,
			&e.Verb, &e.Resource, &e.ResourceID, &e.Old, &e.New, &e.Channel, &e.Client, &e.Agent); err != nil {
			return nil, fmt.Errorf("storage: scan audit row: %w", err)
		}
		e.ActorName = liveOrSnapshot(userIdent(actorUser, actorSvc, actorOutpost), actorSnap)
		e.RealActorName = liveOrSnapshot(userIdent(realUser, realSvc, realOutpost), realSnap)
		out = append(out, e)
	}
	return out, rows.Err()
}

// liveOrSnapshot prefers the identifier the user has NOW and falls back to
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

// WriteAuthEventFor records an authentication event (login, logout, revoke) in
// the audit trail with optional target attribution. Unlike writeAuditRes, it
// takes no transaction: auth events are read/no-tx paths, so the emit is a
// standalone autocommit insert. It records the real actor from context for
// consistency, though auth events are not impersonated. The resource and
// resourceID name what the action targeted (e.g. "user" and the target
// user's id for a session revoke); callers can omit them to default to
// resource='auth' with no target.
func (p *PG) WriteAuthEventFor(ctx context.Context, actorID, verb, resource, resourceID string) error {
	if resource == "" {
		resource = "auth"
	}
	channel, client := auditChannelFrom(ctx)
	if _, err := p.pool.Exec(ctx, `
		insert into audit_log (actor_user_id, real_actor_user_id, actor_username, real_actor_username, verb, resource, resource_id, channel, client, agent)
		values ($1, $2, `+userIdentSQL("$1")+`, `+userIdentSQL("$2")+`, $3, $4, $5, $6, $7, $8)`,
		nullize(actorID), nullize(realActorFrom(ctx)), verb, resource, nullize(resourceID), channel, client, agentFrom(ctx)); err != nil {
		return fmt.Errorf("storage: write auth event: %w", err)
	}
	return nil
}

// WriteAuthEvent records an authentication event (login, logout) in the audit
// trail. Unlike writeAuditRes, it takes no transaction: login and logout are
// read/no-tx paths, so the emit is a standalone autocommit insert. It still
// records the real actor from context, for consistency, though auth events are
// not impersonated.
func (p *PG) WriteAuthEvent(ctx context.Context, actorID, verb string) error {
	return p.WriteAuthEventFor(ctx, actorID, verb, "", "")
}
