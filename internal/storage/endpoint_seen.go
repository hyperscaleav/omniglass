package storage

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// endpointSeenEvery bounds how often an endpoint's last-seen time is written
// (#933): a batch from an endpoint stamped less than this long ago leaves the
// stamp alone, so a busy endpoint costs a write every few seconds, not one
// per batch, and the stamp is honest to within this bound. The same bound,
// for the same reason, as ruleJudgedEvery on event_rule_live.
const endpointSeenEvery = 5 * time.Second

// StampEndpointsSeen records that an ingest batch delivered by each endpoint
// landed (#933). The stamp is the server's clock, not a sample's timestamp
// (a node's clock running ahead cannot carry it into the future), and it is
// written whatever the batch carried: any lane, any series, and a property
// whose unchanged value the transition guard did not write. That is what an
// endpoint's freshness needs and no sample row can say: an instanced series
// is not stamped with the endpoint, and an unchanged property writes nothing.
//
// One statement, coalesced the way event_rule_live is: a row stamped within
// endpointSeenEvery is left alone, a row another batch is stamping right now
// is skipped rather than waited on (SKIP LOCKED), and only an endpoint's
// first sight inserts, ON CONFLICT DO NOTHING, so two batches seeing a new
// endpoint at once never abort each other. It runs in a transaction of its
// own after the batch's sinks have written, so it adds no lock to theirs.
func (p *PG) StampEndpointsSeen(ctx context.Context, endpointIDs []string) error {
	if len(endpointIDs) == 0 {
		return nil
	}
	ids := append([]string(nil), endpointIDs...)
	sort.Strings(ids)
	if _, err := p.pool.Exec(ctx, `with due as (
			select endpoint_id from endpoint_live where endpoint_id = any($1::uuid[]) and last_seen_at <= now() - $2::interval
			order by endpoint_id for update skip locked
		), stamped as (
			update endpoint_live l set last_seen_at = now() from due where l.endpoint_id = due.endpoint_id
		)
		insert into endpoint_live (endpoint_id, last_seen_at)
		select u.id, now() from unnest($1::uuid[]) as u(id)
		where not exists (select 1 from endpoint_live l where l.endpoint_id = u.id)
		  and exists (select 1 from endpoint e where e.id = u.id)
		on conflict (endpoint_id) do nothing`, ids, endpointSeenEvery); err != nil {
		return fmt.Errorf("storage: stamp endpoints seen: %w", err)
	}
	return nil
}
