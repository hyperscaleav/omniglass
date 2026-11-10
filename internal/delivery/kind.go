// Package delivery is the uniform loss measurement (#1641): every telemetry
// kind at every hop reads as one ratio, loss = (in - out) / in, where out is
// what is accounted for (in the store, or held for the operator). A healthy pipeline reads exactly 0 for
// every kind, so the number needs no knowledge of a kind's internals to read.
//
// The package is pure arithmetic and bookkeeping: the node's spool stamps
// each batch from a Ledger, the ingest counts each item's Outcome in a Tally,
// and a reader reconciles the landed receipts of a window (Reconcile) into
// per-kind totals. It does no I/O.
package delivery

// Kind is a telemetry kind, the value a loss row is keyed on (never part of a
// key name).
type Kind string

const (
	Metric   Kind = "metric"
	Property Kind = "property"
	Event    Kind = "event"
	Log      Kind = "log"
)

// Kinds is every kind, in the order a table prints them.
var Kinds = []Kind{Metric, Property, Event, Log}

// Hop is a stretch of the pipeline a kind crosses, measured on its own.
type Hop string

// The hops #1641 names. Only ingest is read from the
// receipts; the node and the server write their own hop's delivery as platform
// metrics (#1659), and the schedule and sim hops follow (#1693, #1694).
const (
	// Schedule is the hop from a run falling due to the run being made.
	Schedule Hop = "schedule"
	// Sim is the hop between a simulated device and the node's task: the
	// lab's closed loop, a lab measure row and never a platform metric.
	Sim Hop = "sim"
	// Node is the hop from a run's output to the stream: the node's spool,
	// settled when the stream acks a batch or the spool gives it up.
	Node Hop = "node"
	// Ingest is the hop from a node's spool to the store: a batch the node
	// minted, landed (or not) by the server.
	Ingest Hop = "ingest"
)

// Hops is every hop name #1641 defines.
var Hops = []Hop{Schedule, Sim, Node, Ingest}
