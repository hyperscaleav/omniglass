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
// names. Every node-only key from probekeys is node-probe, every platform key
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
		for _, member := range []bool{probekeys.IsNodeOnly(key), platformKey, lifecycleKey} {
			if member {
				in++
			}
		}
		if in != 1 {
			t.Errorf("%s is node-only %v, platform %v and command-lifecycle %v; want exactly one", key, probekeys.IsNodeOnly(key), platformKey, lifecycleKey)
		}
		want := NodeProbe
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
	for _, key := range probekeys.NodeOnly() {
		if !slices.Contains(names, key) {
			t.Errorf("node-only %s is not reserved", key)
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
		if c, _ := Reserved(key); c != NodeProbe || strings.TrimSpace(why) == "" {
			t.Errorf("pinnable %s is class %q with reason %q; a pin is a node-probe key's, with its path", key, c, why)
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
		NodeProbe:        {Node},
		Platform:         {Process},
		CommandLifecycle: {Command},
		Calc:             {CalcRule, Operator},
		Collected:        {Node, Push, Fanout, Command, CommandReply, Operator, EventRule},
	}
	for _, c := range Classes() {
		for _, w := range Writers() {
			got := Admits(c, w, "some-key")
			if exp := slices.Contains(want[c], w); got != exp {
				t.Errorf("Admits(%s, %s) = %v, want %v", c, w, got, exp)
			}
		}
	}
	// The one declared row a node-probe key admits: a key pin, on the keys a
	// transport's pin rides (the ssh host key, the tls certificate) alone.
	pins := []string{probekeys.SSHHostKey, probekeys.TLSCertificate}
	for _, key := range probekeys.NodeOnly() {
		want := slices.Contains(pins, key)
		if got := Admits(NodeProbe, KeyPin, key); got != want {
			t.Errorf("Admits(node-probe, key-pin, %s) = %v, want %v", key, got, want)
		}
		if Pinnable(key) != want {
			t.Errorf("Pinnable(%s) = %v, want %v", key, !want, want)
		}
		if Admits(NodeProbe, Operator, key) {
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
		{probekeys.EndpointReachable, false, NodeProbe},
		{probekeys.EndpointReachable, true, NodeProbe},
		{probekeys.CollectionFailed, false, NodeProbe},
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
		{NodeProbe, "tcp-open", Push, "", "only the node"},
		{NodeProbe, "tcp-open", Node, "", "endpoint whose task it ran"},
		{Platform, Health, Push, "", "the health rollup"},
		{CommandLifecycle, CommandIssued, Push, "", "a command's issue"},
		{CommandLifecycle, CommandIssued, Node, "", "a command's issue"},
		{Calc, "avg-temp", Push, "temp-avg", "the calc rule temp-avg"},
		{Collected, "video-input", CalcRule, "", "calc-rule"},
		{NodeProbe, "tcp-open", Fanout, "", "only the node"},
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
