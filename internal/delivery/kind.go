// Package delivery is the uniform loss measurement (#1641): every telemetry
// kind at every hop reads as one ratio, loss = (in - out) / in, where out is
// what is accounted for in the store. A healthy pipeline reads exactly 0 for
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

// Ingest is the hop from a node's spool to the store: a batch the node
// minted, landed (or not) by the server.
const Ingest Hop = "ingest"
