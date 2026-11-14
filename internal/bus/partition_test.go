package bus

import (
	"fmt"
	"hash/fnv"
	"strings"
	"testing"
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
