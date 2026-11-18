package writerclass

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
)

// TestEveryReservedKeyHasExactlyOneClass: the reserved family partitions its
// names. Every outpost-only key from probekeys is outpost-probe, every platform key
// is platform, every command lifecycle event is command-lifecycle, no name is
// in two, and the family is exactly those three sets.
func TestEveryReservedKeyHasExactlyOneClass(t *testing.T) {
	names := ReservedNames()
	if len(names) == 0 {
		t.Fatal("no reserved key")
	}
	if len(slices.Compact(slices.Clone(names))) != len(names) {
		t.Fatalf("a reserved name is listed twice (two classes?): %v", names)
	}
	for _, key := range names {
		c, ok := Reserved(key)
		if !ok || !c.Reserved() {
			t.Errorf("%s is listed reserved but resolves to %q (%v)", key, c, ok)
		}
		_, platformKey := platform[key]
		_, lifecycleKey := commandLifecycle[key]
		in := 0
		for _, member := range []bool{probekeys.IsOutpostOnly(key), platformKey, lifecycleKey} {
			if member {
				in++
			}
		}
		if in != 1 {
			t.Errorf("%s is outpost-only %v, platform %v and command-lifecycle %v; want exactly one", key, probekeys.IsOutpostOnly(key), platformKey, lifecycleKey)
		}
		want := OutpostProbe
		switch {
		case platformKey:
			want = Platform
		case lifecycleKey:
			want = CommandLifecycle
		}
		if c != want {
			t.Errorf("%s resolves to %s, want %s", key, c, want)
		}
	}
	if c, ok := Reserved(CommandIssued); !ok || c != CommandLifecycle {
		t.Errorf("command-issued resolves to %q (%v), want command-lifecycle: only a command's issue writes it", c, ok)
	}
	for key, by := range commandLifecycle {
		if strings.TrimSpace(by) == "" {
			t.Errorf("command lifecycle event %s names no doc", key)
		}
	}
	for _, key := range probekeys.OutpostOnly() {
		if !slices.Contains(names, key) {
			t.Errorf("outpost-only %s is not reserved", key)
		}
	}
	if !slices.Contains(names, probekeys.CollectionFailed) {
		t.Error("collection-failed is not reserved (#1086)")
	}
	for key, by := range platform {
		if strings.TrimSpace(by) == "" {
			t.Errorf("platform key %s names no writer", key)
		}
	}
	for key, why := range pinnable {
		if c, _ := Reserved(key); c != OutpostProbe || strings.TrimSpace(why) == "" {
			t.Errorf("pinnable %s is class %q with reason %q; a pin is an outpost-probe key's, with its path", key, c, why)
		}
	}
}

// TestReservedFailsClosed: a casing or spacing variant of a reserved name is
// reserved, and a near miss is not.
func TestReservedFailsClosed(t *testing.T) {
	for _, key := range ReservedNames() {
		for _, v := range []string{strings.ToUpper(key), " " + key, key + "\t"} {
			if _, ok := Reserved(v); !ok {
				t.Errorf("Reserved(%q) = false, want the variant of %s reserved", v, key)
			}
		}
	}
	for _, v := range []string{"", "uptime", "video-input", "healthy", "tcp", "health-score", "collection"} {
		if c, ok := Reserved(v); ok {
			t.Errorf("Reserved(%q) = %s, want not reserved", v, c)
		}
	}
}

// TestAdmits pins the whole ruling, class by writer: the table is the spec.
func TestAdmits(t *testing.T) {
	want := map[Class][]Writer{
		OutpostProbe:     {Outpost},
		Platform:         {Process},
		CommandLifecycle: {Command},
		Calc:             {CalcRule, Operator},
		Collected:        {Outpost, Push, Fanout, Command, CommandReply, Operator, EventRule},
	}
	for _, c := range Classes() {
		for _, w := range Writers() {
			got := Admits(c, w, "some-key")
			if exp := slices.Contains(want[c], w); got != exp {
				t.Errorf("Admits(%s, %s) = %v, want %v", c, w, got, exp)
			}
		}
	}
	// The one declared row an outpost-probe key admits: a key pin, on the keys a
	// transport's pin rides (the ssh host key, the tls certificate) alone.
	pins := []string{probekeys.SSHHostKey, probekeys.TLSCertificate}
	for _, key := range probekeys.OutpostOnly() {
		want := slices.Contains(pins, key)
		if got := Admits(OutpostProbe, KeyPin, key); got != want {
			t.Errorf("Admits(outpost-probe, key-pin, %s) = %v, want %v", key, got, want)
		}
		if Pinnable(key) != want {
			t.Errorf("Pinnable(%s) = %v, want %v", key, !want, want)
		}
		if Admits(OutpostProbe, Operator, key) {
			t.Errorf("an operator's plain declared value is admitted on %s; only the :retry pin is", key)
		}
	}
	// Every class admits a writer and every writer is admitted somewhere, so
	// no class is unwritable and no writer is dead weight.
	for _, c := range Classes() {
		if len(want[c]) == 0 {
			t.Errorf("class %s admits no writer", c)
		}
	}
	for _, w := range Writers() {
		admitted := w == KeyPin
		for _, c := range Classes() {
			admitted = admitted || Admits(c, w, "")
		}
		if !admitted {
			t.Errorf("writer %s is admitted by no class", w)
		}
	}
}

// TestOf: the reserved class wins over a calc rule's claim (a rule cannot take
// a reserved key), a calc output is calc, and everything else is collected.
func TestOf(t *testing.T) {
	cases := []struct {
		name      string
		calcOwned bool
		want      Class
	}{
		{probekeys.EndpointReachable, false, OutpostProbe},
		{probekeys.EndpointReachable, true, OutpostProbe},
		{probekeys.CollectionFailed, false, OutpostProbe},
		{Health, true, Platform},
		{CommandIssued, false, CommandLifecycle},
		{"avg-temp", true, Calc},
		{"video-input", false, Collected},
	}
	for _, c := range cases {
		if got := Of(c.name, c.calcOwned); got != c.want {
			t.Errorf("Of(%s, %v) = %s, want %s", c.name, c.calcOwned, got, c.want)
		}
	}
}

// TestRefusalNamesKeyAndWriter: every refusal names the key, and the reserved
// and calc ones name who does write it.
func TestRefusalNamesKeyAndWriter(t *testing.T) {
	for _, tc := range []struct {
		c    Class
		key  string
		w    Writer
		rule string
		want string
	}{
		{OutpostProbe, "tcp-open", Push, "", "only the outpost"},
		{OutpostProbe, "tcp-open", Outpost, "", "endpoint whose task it ran"},
		{Platform, Health, Push, "", "the health rollup"},
		{CommandLifecycle, CommandIssued, Push, "", "a command's issue"},
		{CommandLifecycle, CommandIssued, Outpost, "", "a command's issue"},
		{Calc, "avg-temp", Push, "temp-avg", "the calc rule temp-avg"},
		{Collected, "video-input", CalcRule, "", "calc-rule"},
		{OutpostProbe, "tcp-open", Fanout, "", "only the outpost"},
		{Calc, "avg-temp", Fanout, "temp-avg", "the calc rule temp-avg"},
		{Platform, Health, Fanout, "", "the health rollup"},
	} {
		got := Refusal(tc.c, tc.key, tc.w, tc.rule)
		if !strings.Contains(got, tc.key) || !strings.Contains(got, tc.want) {
			t.Errorf("Refusal(%s, %s, %s) = %q, want it to name the key and %q", tc.c, tc.key, tc.w, got, tc.want)
		}
	}
}

// TestFactsRenderTheRuling: the docs facts carry every class with its doc, the
// reserved classes' keys, and exactly the writers Admits admits; and the
// committed render (docs/src/generated/writerclass.json) is current, so the
// page teaching the ruling never drifts from the one enforced.
func TestFactsRenderTheRuling(t *testing.T) {
	facts := Facts()
	if len(facts) != len(Classes()) {
		t.Fatalf("facts carry %d classes, want %d", len(facts), len(Classes()))
	}
	for _, f := range facts {
		if strings.TrimSpace(f.Doc) == "" {
			t.Errorf("class %s has no doc", f.Class)
		}
		if f.Reserved != (len(f.Keys) > 0) {
			t.Errorf("class %s: reserved %v but %d keys", f.Class, f.Reserved, len(f.Keys))
		}
		for _, w := range Writers() {
			listed := slices.ContainsFunc(f.Admits, func(a WriterFact) bool { return a.Writer == w })
			if listed != slices.Contains(admits[f.Class], w) {
				t.Errorf("class %s: facts list %s %v, the ruling %v", f.Class, w, listed, !listed)
			}
		}
		for _, a := range f.Admits {
			if strings.TrimSpace(a.Doc) == "" {
				t.Errorf("writer %s has no doc", a.Writer)
			}
		}
	}
	want, err := FactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("..", "..", "docs", "src", "generated", "writerclass.json"))
	if err != nil {
		t.Fatalf("read the committed render: %v (run make gen)", err)
	}
	if string(got) != string(want) {
		t.Fatal("docs/src/generated/writerclass.json is stale: run make gen")
	}
}

// Each terminal status a command records writes its own lifecycle event, a
// reserved key only a command writes; a status that is not terminal writes
// none.
func TestCommandOutcomeEvents(t *testing.T) {
	for status, want := range map[string]string{"settled": CommandSettled, "failed": CommandFailed, "timed-out": CommandTimedOut} {
		got, ok := CommandOutcomeEvent(status)
		if !ok || got != want {
			t.Errorf("CommandOutcomeEvent(%q) = %q, %v; want %q", status, got, ok, want)
		}
		if c, reserved := Reserved(got); !reserved || c != CommandLifecycle {
			t.Errorf("%s resolves to %q (%v), want command-lifecycle", got, c, reserved)
		}
		if !Admits(CommandLifecycle, Command, got) || Admits(CommandLifecycle, EventRule, got) || Admits(CommandLifecycle, Push, got) {
			t.Errorf("%s admits a writer other than a command", got)
		}
	}
	if got, ok := CommandOutcomeEvent("issued"); ok {
		t.Errorf("issued writes %q, want no outcome event", got)
	}
}

// The webhook receiver's counts (#1369) are platform keys: the server counts
// the posts it accepts and refuses, so neither an outpost nor a push may write
// them, and the process writes them through the gateway.
func TestTheWebhookCountsArePlatformKeys(t *testing.T) {
	for _, key := range []string{WebhookPostCount, WebhookRefusalCount} {
		if c, ok := Reserved(key); !ok || c != Platform {
			t.Errorf("%s resolves to %q (%v), want platform", key, c, ok)
		}
		if !Admits(Platform, Process, key) {
			t.Errorf("the process may not write %s", key)
		}
		for _, w := range []Writer{Outpost, Push, Operator} {
			if Admits(Platform, w, key) {
				t.Errorf("%s may write %s, which only the webhook receiver counts", w, key)
			}
		}
	}
}

// The platform's self-monitoring keys (#1625) are platform keys with no
// prefix, written by the process alone. An outpost's self-report lane admits only
// the keys an outpost reports of itself: never health, a webhook count, or the
// server's checkin-age, so an outpost cannot speak for another writer.
func TestTheOutpostSelfKeysArePlatformKeys(t *testing.T) {
	keys := OutpostSelfKeys()
	if len(keys) != 20 {
		t.Fatalf("outpost-self keys %v, want the 20 an outpost reports of itself", keys)
	}
	for _, key := range append(keys, CheckinAge) {
		if strings.Contains(key, ".") || strings.HasPrefix(key, "platform") || strings.HasPrefix(key, "outpost-") {
			t.Errorf("%s carries a prefix; platform keys carry none", key)
		}
		if c, ok := Reserved(key); !ok || c != Platform {
			t.Errorf("%s resolves to %q (%v), want platform", key, c, ok)
		}
		if !Admits(Platform, Process, key) || Admits(Platform, Outpost, key) || Admits(Platform, Push, key) {
			t.Errorf("%s: only the process writes a platform key", key)
		}
	}
	for _, key := range keys {
		if !OutpostSelf(key) {
			t.Errorf("%s is not admitted on the outpost-self lane", key)
		}
	}
	for _, key := range []string{Health, WebhookPostCount, CheckinAge, "poll-duration", "TICK-DURATION", "made-up"} {
		if OutpostSelf(key) {
			t.Errorf("the outpost-self lane admits %s", key)
		}
	}
}

// TestTheRetentionKeysArePlatformKeys (#1629): the retention pass's figures
// are platform keys with no prefix, written by the process alone, and none is
// a key an outpost reports of itself.
func TestTheRetentionKeysArePlatformKeys(t *testing.T) {
	keys := RetentionKeys()
	want := []string{DatabaseBytes, MetricBlockMarkLag, MetricOldestDayAge, MetricPartitionCount,
		MetricPartitionCreatedCount, MetricPartitionDroppedCount, MetricTrendMarkLag, RetentionBehind,
		RetentionDeletedCount, RetentionPassDuration, RetentionPassFailureCount, RetentionWindowDays, TableBytes}
	slices.Sort(want)
	if !slices.Equal(keys, want) {
		t.Fatalf("retention keys %v, want the 13 the pass reports %v", keys, want)
	}
	for _, key := range keys {
		if c, ok := Reserved(key); !ok || c != Platform {
			t.Errorf("%s resolves to %q (%v), want platform", key, c, ok)
		}
		if !Admits(Platform, Process, key) || Admits(Platform, Outpost, key) || Admits(Platform, Push, key) || Admits(Platform, Operator, key) {
			t.Errorf("%s: only the process writes a retention key", key)
		}
		if OutpostSelf(key) {
			t.Errorf("the outpost-self lane admits %s", key)
		}
	}
}

// TestServerKeysAreTheProcesssAlone: the server's request and database keys
// (#1627) are platform keys only the process writes, and an outpost naming one on
// its outpost-self lane is refused like any key not its own.
func TestServerKeysAreTheProcesssAlone(t *testing.T) {
	keys := []string{HTTPRequestCount, HTTPRequestDuration, HTTPRequestDurationMax, DBSlowQueryCount,
		DBPoolAcquired, DBPoolIdle, DBPoolTotal, DBPoolMax, DBPoolWaitCount, DBPoolWaitDuration}
	for _, key := range keys {
		if c, ok := Reserved(key); !ok || c != Platform {
			t.Errorf("%s resolves to %q (%v), want platform", key, c, ok)
		}
		if !Admits(Platform, Process, key) || Admits(Platform, Outpost, key) || Admits(Platform, Push, key) {
			t.Errorf("%s: only the process writes a server key", key)
		}
		if OutpostSelf(key) {
			t.Errorf("the outpost-self lane admits %s", key)
		}
	}
}

// TestServerPipelineKeysAreTheProcesssAlone: the server's pipeline keys
// (#1628) are platform keys only the process writes, and an outpost naming one on
// its outpost-self lane is refused.
func TestServerPipelineKeysAreTheProcesssAlone(t *testing.T) {
	keys := []string{IngestLag, IngestLagMax, IngestBatchCount, IngestSampleCount, IngestLaneBusy,
		BusConsumerPending, BusConsumerAckPending, BusConsumerRedelivered, BusTelemetryAckFailureCount,
		BusWorklistPullCount, BusWorklistReplyBytesMax, BusRequestCount,
		BusRequestFailureCount, CalcPendingDepth, CalcServedCount, CalcPassDuration, OutboxQueuedDepth,
		OutboxDueAge, SweepDuration, SweepCount, SweepFailureCount}
	for _, key := range keys {
		if c, ok := Reserved(key); !ok || c != Platform {
			t.Errorf("%s resolves to %q (%v), want platform", key, c, ok)
		}
		if !Admits(Platform, Process, key) || Admits(Platform, Outpost, key) || Admits(Platform, Push, key) {
			t.Errorf("%s: only the process writes a pipeline key", key)
		}
		if OutpostSelf(key) {
			t.Errorf("the outpost-self lane admits %s", key)
		}
	}
	for _, key := range []string{ProcessCPUSeconds, ProcessFDCount} {
		if !OutpostSelf(key) {
			t.Errorf("%s is a generic process key an outpost reports of itself", key)
		}
	}
}

// An outpost's command queue (#1626) is platform keys the process writes. The
// server measures the queue and the wire, so only the pull-failure count, the
// one fact the outpost alone sees, rides the outpost-self lane.
func TestTheCommandQueueKeysArePlatformKeys(t *testing.T) {
	server := []string{CommandPendingCount, CommandPendingAge, CommandDispatchLatency, CommandExecutionLatency,
		CommandRedeliveryCount, CommandStuckCount, CommandExpiredCount}
	for _, key := range append(server, CommandPullFailureCount) {
		if c, ok := Reserved(key); !ok || c != Platform {
			t.Errorf("%s resolves to %q (%v), want platform", key, c, ok)
		}
		if !Admits(Platform, Process, key) {
			t.Errorf("the process may not write %s", key)
		}
		for _, w := range []Writer{Outpost, Push, Operator, CalcRule} {
			if Admits(Platform, w, key) {
				t.Errorf("%s may write %s", w, key)
			}
		}
	}
	for _, key := range server {
		if OutpostSelf(key) {
			t.Errorf("the outpost-self lane admits %s, which the server measures", key)
		}
	}
	if !OutpostSelf(CommandPullFailureCount) {
		t.Errorf("the outpost-self lane refuses %s", CommandPullFailureCount)
	}
}

// TestTheServerIdentityKeysAreTheBootsAlone: the server's build and start
// time (#1644) are platform keys only the server's boot writes, so an outpost's
// probe lane or a push naming either is refused with the platform-key reason,
// which names the boot as the key's writer.
func TestTheServerIdentityKeysAreTheBootsAlone(t *testing.T) {
	for _, key := range []string{ServerVersion, ServerStartedAt} {
		if c, ok := Reserved(key); !ok || c != Platform {
			t.Errorf("%s resolves to %q (%v), want platform", key, c, ok)
		}
		if !Admits(Platform, Process, key) {
			t.Errorf("the process may not write %s", key)
		}
		for _, w := range []Writer{Outpost, Push, Fanout, Operator, CalcRule} {
			if Admits(Platform, w, key) {
				t.Errorf("%s may write %s", w, key)
			}
			if got := Refusal(Platform, key, w, ""); got != key+" is a platform key: only the server boot writes it" {
				t.Errorf("Refusal(%s by %s) = %q", key, w, got)
			}
		}
		if OutpostSelf(key) {
			t.Errorf("the outpost-self lane admits %s", key)
		}
	}
}

// TestTheDeliveryKeysAreEveryHopsOwn: a hop's delivery keys (#1659) are
// platform keys the process alone writes, of itself: an outpost on its outpost-self
// lane, the server on its own component. They replace the folded drop and
// failure counts, which are no longer declared.
func TestTheDeliveryKeysAreEveryHopsOwn(t *testing.T) {
	for _, key := range []string{DeliveryInCount, DeliveryOutCount, DeliveryLossRatio} {
		if c, ok := Reserved(key); !ok || c != Platform {
			t.Errorf("%s resolves to %q (%v), want platform", key, c, ok)
		}
		if !Admits(Platform, Process, key) || Admits(Platform, Outpost, key) || Admits(Platform, Push, key) {
			t.Errorf("%s: only the process writes a delivery key", key)
		}
		if !OutpostSelf(key) {
			t.Errorf("the outpost-self lane refuses %s", key)
		}
	}
	for _, key := range []string{"spool-drop-count", "publish-failure-count", "bus-telemetry-drop-count"} {
		if _, ok := Reserved(key); ok {
			t.Errorf("%s is still declared; the delivery keys fold it", key)
		}
	}
}
