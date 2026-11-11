package delivery

// Outcome is what became of one item. Each item takes exactly one. Names are
// kebab-case and stable: they print in the loss table and its JSON.
type Outcome string

const (
	// Stored is in the store.
	Stored Outcome = "stored"
	// Coalesced is a property value equal to its series' latest, which the
	// transition-only guard skipped on purpose: the information is stored.
	Coalesced Outcome = "coalesced"
	// Deduplicated is a redelivered copy the receipt absorbed. It reads
	// beside the ratio, never in it, so a retry storm neither dilutes nor
	// hides real loss.
	Deduplicated Outcome = "deduplicated"

	// RefusedUnregistered names a key no type registers.
	RefusedUnregistered Outcome = "refused-unregistered"
	// RefusedWriter is a key whose writer class refuses the writer.
	RefusedWriter Outcome = "refused-writer"
	// RefusedInstance is a node-probe sample at an instance the probe does
	// not own.
	RefusedInstance Outcome = "refused-instance"
	// RefusedInvalid is a value or payload its type's validation refuses.
	RefusedInvalid Outcome = "refused-invalid"
	// RefusedEmpty is an event with neither a message nor a payload.
	RefusedEmpty Outcome = "refused-empty"
	// RefusedWrite is a group the gateway refused while its siblings landed.
	RefusedWrite Outcome = "refused-write"
	// Orphaned is a group whose task's owner is gone.
	Orphaned Outcome = "orphaned"
	// Unbound is an identified sample whose identity has no binding. It is
	// not stored, but it is accounted for, not lost: the operator has not
	// assigned the identity, and the conduit's unbound inbox surfaces it
	// (count, keys, heard count), as configuration state rather than data
	// the hop dropped.
	Unbound Outcome = "unbound"
	// Unidentified is an identified sample with no conduit, nothing
	// resolved, or no valid identity.
	Unidentified Outcome = "unidentified"
	// RefusedOwner is an item in a batch the server Termed whole because it
	// asserted an owner on the node lane, which only the API lane may do.
	RefusedOwner Outcome = "refused-owner"
	// RefusedTask is an item in a batch the server Termed whole because it
	// carried task rows outside its groups.
	RefusedTask Outcome = "refused-task"
	// RefusedBatch is an item in a batch the gateway refused whole.
	RefusedBatch Outcome = "refused-batch"
	// RefusedMetadata is an item in a batch the server Termed whole because
	// its delivery metadata was unreadable, so it could not count attempts.
	RefusedMetadata Outcome = "refused-metadata"
	// Abandoned is an item in a batch that failed to land on every attempt
	// the consumer allows and was Termed at the last.
	Abandoned Outcome = "abandoned"
	// Missing is an item that never landed at ingest: a gap in its boot's
	// offsets the node does not account for, or a shortfall between what a
	// batch declared and what it decoded to.
	Missing Outcome = "missing"

	// The node hop's outcomes (#1692): an item the node minted into a batch
	// either shipped or was dropped by the node under one of these causes.

	// Shipped is an item the node's spool handed to the stream: out of the
	// node hop, and into the ingest hop.
	Shipped Outcome = "shipped"
	// SpoolOverflow is an item in a batch the spool dropped, oldest first,
	// at its count or bytes cap.
	SpoolOverflow Outcome = "spool-overflow"
	// SpoolExpired is an item in a batch the spool dropped at its age cap.
	SpoolExpired Outcome = "spool-expired"
	// BusRefused is an item in a batch the bus refused outright (too large,
	// no stream), which no retry would change.
	BusRefused Outcome = "bus-refused"
	// SelfLogOverflow is a node self-log line the buffer dropped at its cap
	// before a batch carried it.
	SelfLogOverflow Outcome = "self-log-overflow"
	// EncodeFailed is an item in a group or batch the node could not encode.
	EncodeFailed Outcome = "encode-failed"
)

// The sim hop's outcomes (#1694). The sim hop counts requests a simulated
// endpoint settled, not telemetry items (the sim cannot know which kinds an
// answer would have carried), so it has its own set, never mixed into the
// ingest rows. A refusal or an error the endpoint sent back is returned: it
// reached the node's task, which reports it; it is not the hop's loss.
const (
	// Returned is a request whose reply reached the node's task.
	Returned Outcome = "returned"
	// Withheld is a request the sim chose not to answer: a credential it
	// does not accept, or its own configured datagram loss.
	Withheld Outcome = "withheld"
	// Dropped is a reply the sim could not write to its socket.
	Dropped Outcome = "dropped"
	// NotReturned is a reply the sim sent that never reached the node's
	// task: lost on the way back, or late past the task's timeout.
	NotReturned Outcome = "not-returned"
)

// The publish hop's outcomes (#1746). The server's publisher counts calls
// that reached the push lane's turn, never items: a refused call minted
// nothing, so the items it carried are on no ledger and are no receipt's.
// They read here, under refused-turn, and never as the ingest hop's missing.
const (
	// TurnTaken is a call that took the publish turn: out of the publish hop.
	TurnTaken Outcome = "turn-taken"
	// RefusedTurn is a call whose wait for the turn ran out, answered 503
	// before it minted anything: the publish hop's loss.
	RefusedTurn Outcome = "refused-turn"
)

// PublishOutcomes is every publish hop outcome, out first, then loss.
var PublishOutcomes = []Outcome{TurnTaken, RefusedTurn}

// SimOutcomes is every sim hop outcome, out first, then loss.
var SimOutcomes = []Outcome{Returned, Withheld, Dropped, NotReturned}

// IngestOutcomes is every outcome of the ingest hop, out first, then the
// dedup count, then loss.
var IngestOutcomes = []Outcome{
	Stored, Coalesced, Unbound, Deduplicated,
	RefusedUnregistered, RefusedWriter, RefusedInstance, RefusedInvalid,
	RefusedEmpty, RefusedWrite, Orphaned, Unidentified,
	RefusedOwner, RefusedTask, RefusedBatch, RefusedMetadata, Abandoned, Missing,
}

// NodeOutcomes is every outcome of the node hop, out first, then loss.
var NodeOutcomes = []Outcome{
	Shipped, SpoolOverflow, SpoolExpired, BusRefused, SelfLogOverflow, EncodeFailed,
}

// Outcomes is every item outcome of every hop: the ingest hop's, then the
// node's. The sim hop counts requests, not items, so SimOutcomes stands apart.
var Outcomes = append(append([]Outcome{}, IngestOutcomes...), NodeOutcomes...)

// OutcomesOf is the outcomes a hop's row counts, nil for a hop with none.
func OutcomesOf(h Hop) []Outcome {
	switch h {
	case Sim:
		return SimOutcomes
	case Node:
		return NodeOutcomes
	case Ingest:
		return IngestOutcomes
	case Publish:
		return PublishOutcomes
	}
	return nil
}

// Out reports whether the outcome counts as out: accounted for, in the store
// or, for an unbound sample, in its conduit's unbound inbox, or at the node
// hop handed to the stream, or, on the sim hop, back at the node, or, on the
// publish hop, through the turn.
func (o Outcome) Out() bool {
	return o == Stored || o == Coalesced || o == Unbound || o == Shipped || o == Returned || o == TurnTaken
}

// Deduplicated reports whether the outcome is the redelivery count, reported
// beside the ratio.
func (o Outcome) Deduplicated() bool { return o == Deduplicated }

// Lost reports whether the outcome counts as loss. A refusal is loss on
// purpose: counted as out, a fleet refusing a third of its samples would read
// 0, and only someone who knew to look elsewhere would notice.
func (o Outcome) Lost() bool { return !o.Out() && !o.Deduplicated() }
