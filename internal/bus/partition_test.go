package bus

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
)

// A configured lane count is a whole number from 1 to 64, read with its
// whitespace trimmed; empty is the default; anything else is refused.
func TestTheIngestPartitionCountIsAWholeNumberInRange(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want int
		bad  bool
	}{
		{"", DefaultIngestPartitions, false},
		{"  ", DefaultIngestPartitions, false},
		{"4", 4, false},
		{" 8 ", 8, false},
		{"1", 1, false},
		{"64", 64, false},
		{"0", 0, true},
		{"65", 0, true},
		{"-2", 0, true},
		{"x", 0, true},
		{"2.5", 0, true},
	} {
		got, err := ParseIngestPartitions(c.raw)
		if c.bad {
			if err == nil {
				t.Errorf("ParseIngestPartitions(%q) = %d, want a refusal", c.raw, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("ParseIngestPartitions(%q) = %d, %v; want %d", c.raw, got, err, c.want)
		}
	}
}

// The lane count is the asked-for one, or the default, held under the
// database pool less one connection, and the reason names which.
func TestTheIngestPartitionsAreHeldUnderTheDatabasePool(t *testing.T) {
	for _, c := range []struct {
		want, db, got int
		reason        string
	}{
		{0, 0, DefaultIngestPartitions, "default"},
		{0, 20, DefaultIngestPartitions, "default"},
		{8, 20, 8, "set"},
		{8, 5, 4, "held under the database pool"},
		{0, 3, 2, "held under the database pool"},
		{4, 1, 1, "held under the database pool"},
		{4, 2, 1, "held under the database pool"},
	} {
		got, reason := ingestPartitions(c.want, c.db)
		if got != c.got || !strings.Contains(reason, c.reason) {
			t.Errorf("ingestPartitions(%d, %d) = %d, %q; want %d naming %q", c.want, c.db, got, reason, c.got, c.reason)
		}
	}
}

// A node's partition is the one the stream's partition transform files it
// under: FNV-1a of its name, modulo the count, so the server reads the same
// lane the stream wrote.
func TestANodesPartitionIsTheStreamsHash(t *testing.T) {
	for _, node := range []string{"node-a", "node-b", "node-ledger", "corp-site-17-node-2", ""} {
		for _, k := range []int{1, 2, 4, 7, 64} {
			h := fnv.New32a()
			_, _ = h.Write([]byte(node))
			want := int(h.Sum32() % uint32(k))
			if got := PartitionOf(node, k); got != want {
				t.Errorf("PartitionOf(%q, %d) = %d, want %d", node, k, got, want)
			}
		}
	}
	for i := range 200 {
		if p := PartitionOf(fmt.Sprintf("n-%d", i), 4); p < 0 || p >= 4 {
			t.Fatalf("PartitionOf(n-%d, 4) = %d, outside 0..3", i, p)
		}
	}
}

// The stream's transform, each partition's durable and its filter are named
// by the partition's index.
func TestEachPartitionHasItsOwnDurableAndFilter(t *testing.T) {
	if got := partitionDurable(3); got != "og-telemetry-worker-3" {
		t.Errorf("partitionDurable(3) = %q", got)
	}
	if got := partitionFilter(3); got != "og.v1.telemetry.3.*" {
		t.Errorf("partitionFilter(3) = %q", got)
	}
	if got := partitionTransform(4).Destination; got != "og.v1.telemetry.{{partition(4,1)}}.{{wildcard(1)}}" {
		t.Errorf("partitionTransform(4).Destination = %q", got)
	}
	if got := partitionTransform(4).Source; got != "og.v1.telemetry.*" {
		t.Errorf("partitionTransform(4).Source = %q", got)
	}
}

// A batch's receipt key names the node's own subject whichever partition the
// stream filed it under, so a key read back is the same at any lane count.
func TestABatchKeyNamesTheNodesOwnSubject(t *testing.T) {
	for _, c := range []struct{ subject, want string }{
		{"og.v1.telemetry.2.node-a", "og.v1.telemetry.node-a"},
		{"og.v1.telemetry.node-a", "og.v1.telemetry.node-a"},
		{"og.v1.api.telemetry", "og.v1.api.telemetry"},
	} {
		if got := canonicalSubject(c.subject); got != c.want {
			t.Errorf("canonicalSubject(%q) = %q, want %q", c.subject, got, c.want)
		}
	}
}

// The partition count a stream files by is read back off its transform: the
// count partitionTransform wrote, or none for a stream that files no batch by
// partition.
func TestAStreamsPartitionCountIsReadOffItsTransform(t *testing.T) {
	for _, k := range []int{1, 4, 64} {
		tr := partitionTransform(k)
		if got := transformPartitions(&tr); got != k {
			t.Errorf("transformPartitions(partitionTransform(%d)) = %d", k, got)
		}
	}
	for _, tr := range []*jetstream.SubjectTransformConfig{
		nil,
		{},
		{Source: "og.v1.telemetry.*", Destination: "og.v1.telemetry.{{wildcard(1)}}"},
		{Source: "og.v1.telemetry.*", Destination: "og.v1.telemetry.{{partition(x,1)}}.{{wildcard(1)}}"},
	} {
		if got := transformPartitions(tr); got != 0 {
			t.Errorf("transformPartitions(%+v) = %d, want 0", tr, got)
		}
	}
}

// The partitions a start drains past its count are every one a larger prior
// count filed batches under, durable or not, and every durable past the
// count, each once and in order; none when the count did not shrink.
func TestThePartitionsPastTheCountAreThePriorsAndTheDurables(t *testing.T) {
	for _, c := range []struct {
		k, prior int
		durables []string
		want     []int
	}{
		{4, 4, []string{"og-telemetry-worker", "og-telemetry-worker-0", "og-telemetry-worker-3"}, nil},
		{4, 0, nil, nil},
		{4, 8, nil, []int{4, 5, 6, 7}},
		{4, 8, []string{"og-telemetry-worker-5", "og-telemetry-worker-9", "og-telemetry-worker-1"}, []int{4, 5, 6, 7, 9}},
		{2, 2, []string{"og-telemetry-worker-07", "og-telemetry-worker-x", "og-telemetry-worker-3", "other-5"}, []int{3}},
		{8, 4, []string{"og-telemetry-worker-2"}, nil},
	} {
		got := pastPartitions(c.k, c.prior, c.durables)
		if !slices.Equal(got, c.want) {
			t.Errorf("pastPartitions(%d, %d, %v) = %v, want %v", c.k, c.prior, c.durables, got, c.want)
		}
	}
}

// A start that files by a different count than the stream did while it still
// holds batches may land a node's earlier batches after its later ones: its
// earlier batches sit under the old partition, its later ones under the new.
// The same count, an empty stream or a new stream risks nothing.
func TestAChangedCountOverAHeldBacklogRisksANodesOrder(t *testing.T) {
	for _, c := range []struct {
		prior, k int
		held     uint64
		want     bool
	}{
		{4, 4, 100, false},
		{4, 8, 0, false},
		{4, 8, 1, true},
		{8, 4, 3, true},
		{0, 4, 2, true},
		{-1, 4, 0, false},
	} {
		if got := reorderRisk(c.prior, c.k, c.held); got != c.want {
			t.Errorf("reorderRisk(%d, %d, %d) = %v, want %v", c.prior, c.k, c.held, got, c.want)
		}
	}
}
