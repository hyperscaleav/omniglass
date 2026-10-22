package storage

import (
	"context"
	"fmt"
)

// endpointLive is the endpoint's coalesced last-seen stamp. An endpoint can be
// deleted while a batch it delivered is in flight, so first sight checks the
// endpoint row still exists.
var endpointLive = liveStamp{table: "endpoint_live", key: "endpoint_id", at: "last_seen_at", owner: "endpoint"}

// StampEndpointsSeen records that an ingest batch delivered by each endpoint
// landed (#933). The stamp is the server's clock, not a sample's timestamp
// (a node's clock running ahead cannot carry it into the future), and it is
// written whatever the batch carried: any lane, any series, and a property
// whose unchanged value the transition guard did not write. That is what an
// endpoint's freshness needs and no sample row can say: an instanced series
// is not stamped with the endpoint, and an unchanged property writes nothing.
//
// One statement, the coalesced live stamp (liveStamp, #943): a row stamped
// within liveStampEvery is left alone, a row another batch is stamping right
// now is skipped rather than waited on, and only an endpoint's first sight
// inserts, so two batches seeing a new endpoint at once never abort each
// other. It runs in a transaction of its own after the batch's sinks have
// written, so it adds no lock to theirs.
func (p *PG) StampEndpointsSeen(ctx context.Context, endpointIDs []string) error {
	if err := endpointLive.stamp(ctx, p.pool, endpointIDs, liveStampEvery); err != nil {
		return fmt.Errorf("storage: stamp endpoints seen: %w", err)
	}
	return nil
}
