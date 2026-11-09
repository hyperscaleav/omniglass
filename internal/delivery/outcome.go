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
	// Unbound is an identified sample whose identity has no binding: the
	// conduit's inbox counts it, but the sample is not stored.
	Unbound Outcome = "unbound"
	// Unidentified is an identified sample with no conduit, nothing
	// resolved, or no valid identity.
	Unidentified Outcome = "unidentified"
	// Missing is an item that never landed: a gap in its boot's offsets, or
	// a shortfall between what a batch declared and what it decoded to.
	Missing Outcome = "missing"
)

// Outcomes is every outcome, out first, then the dedup count, then loss.
var Outcomes = []Outcome{
	Stored, Coalesced, Deduplicated,
	RefusedUnregistered, RefusedWriter, RefusedInstance, RefusedInvalid,
	RefusedEmpty, RefusedWrite, Orphaned, Unbound, Unidentified, Missing,
}

// Out reports whether the outcome counts as out: accounted for in the store.
func (o Outcome) Out() bool { return o == Stored || o == Coalesced }

// Deduplicated reports whether the outcome is the redelivery count, reported
// beside the ratio.
func (o Outcome) Deduplicated() bool { return o == Deduplicated }

// Lost reports whether the outcome counts as loss. A refusal is loss on
// purpose: counted as out, a fleet refusing a third of its samples would read
// 0, and only someone who knew to look elsewhere would notice.
func (o Outcome) Lost() bool { return !o.Out() && !o.Deduplicated() }
