package storage

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// liveStampEvery bounds how often a coalesced last-seen stamp is written: a
// batch touching a row stamped less than this long ago leaves the stamp
// alone, so a busy key costs a write every few seconds, not one per batch,
// and the stamp is honest to within this bound.
const liveStampEvery = 5 * time.Second

// liveStamp names one coalesced last-seen table (#943): a row per key, kept
// off the key's own row so ingest never contends with an edit to it.
// event_rule_live (#929) and endpoint_live (#933) are two; a third live
// stamp is a liveStamp value and a call, not a copy of the statement.
type liveStamp struct {
	// table is the live table, key its uuid primary key column, and at the
	// timestamptz column the stamp writes.
	table, key, at string
	// owner, when set, is the table key references: an id with no owner row
	// (deleted while its batch was in flight) is skipped rather than failing
	// the insert's foreign key. Empty when the caller already holds the owner.
	owner string
}

// stamp records, by the server's clock, that each id was seen: one statement.
// A row stamped within every is left alone, a row another transaction is
// stamping right now is skipped rather than waited on (FOR UPDATE SKIP
// LOCKED), and only an id's first sight inserts, ON CONFLICT DO NOTHING, so
// two batches seeing a new id at once never abort each other. Run on a
// transaction, now() is that transaction's clock; on the pool, the
// statement's own. Ids are locked in sorted order. The error is the
// statement's, unwrapped: each caller names its own stamp.
func (s liveStamp) stamp(ctx context.Context, q txQuerier, ids []string, every time.Duration) error {
	if len(ids) == 0 {
		return nil
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	table := pgx.Identifier{s.table}.Sanitize()
	key := pgx.Identifier{s.key}.Sanitize()
	at := pgx.Identifier{s.at}.Sanitize()
	guard := ""
	if s.owner != "" {
		guard = fmt.Sprintf("\n\t\t  and exists (select 1 from %s o where o.id = u.id)", pgx.Identifier{s.owner}.Sanitize())
	}
	sql := fmt.Sprintf(`with due as (
			select %[2]s from %[1]s where %[2]s = any($1::uuid[]) and %[3]s <= now() - $2::interval
			order by %[2]s for update skip locked
		), stamped as (
			update %[1]s l set %[3]s = now() from due where l.%[2]s = due.%[2]s
		)
		insert into %[1]s (%[2]s, %[3]s)
		select u.id, now() from unnest($1::uuid[]) as u(id)
		where not exists (select 1 from %[1]s l where l.%[2]s = u.id)%[4]s
		on conflict (%[2]s) do nothing`, table, key, at, guard)
	_, err := q.Exec(ctx, sql, sorted, every)
	return err
}
