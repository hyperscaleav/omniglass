package bus

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/nats-io/nats.go/jetstream"
)

// The ingest partitions (#1896, ADR-1896). One durable with one batch in
// flight keeps every node's batches in order, but it lands one batch at a
// time for the whole fleet, so its throughput is one landing's latency. The
// stream instead files each node's batches under a partition of the node's
// name, and each partition has a durable of its own with one batch in
// flight: a node's batches still land in the order it sent them (they share
// a partition), while the partitions land side by side.

// DefaultIngestPartitions is how many ingest lanes the server runs when it
// is not told otherwise.
const DefaultIngestPartitions = 4

// maxIngestPartitions bounds a configured lane count.
const maxIngestPartitions = 64

// ParseIngestPartitions reads a configured lane count: a whole number from 1
// to 64, whitespace trimmed, or the default when empty. Anything else is
// refused, so a typo fails the boot rather than running some other count.
func ParseIngestPartitions(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultIngestPartitions, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxIngestPartitions {
		return 0, fmt.Errorf("ingest partitions %q: want a whole number from 1 to %d", raw, maxIngestPartitions)
	}
	return n, nil
}

// ingestPartitions is the lane count the server runs and why: want, or the
// default when want is not positive, held under the database pool less one
// connection (no cap when dbPool is not positive), and never under one. Each
// lane holds a connection while it lands a batch, so more lanes than the pool
// would only queue on it.
func ingestPartitions(want, dbPool int) (int, string) {
	n, reason := want, "set"
	if n <= 0 {
		n, reason = DefaultIngestPartitions, "default"
	}
	if dbPool > 0 && n > dbPool-1 {
		return max(dbPool-1, 1), fmt.Sprintf("held under the database pool of %d", dbPool)
	}
	return n, reason
}

// PartitionOf is the partition the stream files node's batches under among
// k: FNV-1a of the name, modulo k, the hash the stream's partition transform
// computes, so the server and the stream agree on a node's lane.
func PartitionOf(node string, k int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(node))
	return int(h.Sum32() % uint32(k))
}

// telemetryPrefix is the node telemetry subjects' common prefix.
var telemetryPrefix = strings.TrimSuffix(collection.TelemetryWildcard, "*")

// partitionDurable is partition i's durable.
func partitionDurable(i int) string { return fmt.Sprintf("%s-%d", telemetryConsumer, i) }

// partitionIndex reads the partition a durable named by partitionDurable
// serves; false for any other durable.
func partitionIndex(durable string) (int, bool) {
	rest, ok := strings.CutPrefix(durable, telemetryConsumer+"-")
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(rest)
	if err != nil || i < 0 || strconv.Itoa(i) != rest {
		return 0, false
	}
	return i, true
}

// partitionFilter is the subject partition i's durable takes: every node's
// batch the stream filed under i.
func partitionFilter(i int) string { return fmt.Sprintf("%s%d.*", telemetryPrefix, i) }

// partitionTransform files a node's telemetry subject under its partition
// among k as the stream stores it: og.v1.telemetry.<node> is stored as
// og.v1.telemetry.<partition>.<node>.
func partitionTransform(k int) jetstream.SubjectTransformConfig {
	return jetstream.SubjectTransformConfig{
		Source:      collection.TelemetryWildcard,
		Destination: fmt.Sprintf("%s{{partition(%d,1)}}.{{wildcard(1)}}", telemetryPrefix, k),
	}
}

// transformPartitions is the partition count a stream's transform files node
// batches by, as partitionTransform wrote it; 0 for a stream with no such
// transform, which files each batch under the node's own subject.
func transformPartitions(t *jetstream.SubjectTransformConfig) int {
	if t == nil {
		return 0
	}
	_, rest, ok := strings.Cut(t.Destination, "{{partition(")
	if !ok {
		return 0
	}
	n, _, ok := strings.Cut(rest, ",")
	if !ok {
		return 0
	}
	k, err := strconv.Atoi(n)
	if err != nil || k < 1 {
		return 0
	}
	return k
}

// pastPartitions is every partition at or past k a start drains, in order:
// each a prior count of prior filed batches under, whether or not its durable
// is left, and each partition durable among durables at or past k.
func pastPartitions(k, prior int, durables []string) []int {
	var out []int
	for i := k; i < prior; i++ {
		out = append(out, i)
	}
	for _, name := range durables {
		if i, ok := partitionIndex(name); ok && i >= k {
			out = append(out, i)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// reorderRisk is whether a start that files by k partitions may land a node's
// batches out of order: the stream filed the batches it holds by prior (0 for
// none, -1 for no stream before this start), and a node's batches held under
// prior's partition land beside the ones the stream files under k's, on
// another lane, in no fixed order.
func reorderRisk(prior, k int, held uint64) bool {
	return prior >= 0 && prior != k && held > 0
}

// canonicalSubject is the subject a node published a batch on, whichever
// partition the stream filed it under: og.v1.telemetry.<p>.<node> reads as
// og.v1.telemetry.<node>; any other subject is its own.
func canonicalSubject(subject string) string {
	rest, ok := strings.CutPrefix(subject, telemetryPrefix)
	if !ok {
		return subject
	}
	if _, node, ok := strings.Cut(rest, "."); ok {
		return collection.TelemetrySubject(node)
	}
	return subject
}
