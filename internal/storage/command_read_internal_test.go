package storage

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// Where an in-flight command sits follows from the row: queued until a node
// takes it, then delivered or awaiting the reply by how it settles, then
// awaiting the observation; a command nothing can carry awaits observation
// from its issue; a terminal command's phase is its status.
func TestCommandPhase(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status, mode                     string
		delivered, executed, deliverable bool
		want                             string
	}{
		{"issued", SettleModeReply, false, false, true, PhaseQueued},
		{"issued", SettleModeObservation, false, false, true, PhaseQueued},
		{"issued", SettleModeReply, true, false, true, PhaseAwaitingReply},
		{"issued", SettleModeObservation, true, false, true, PhaseDelivered},
		{"issued", SettleModeObservation, true, true, true, PhaseAwaitingObservation},
		{"issued", SettleModeReply, true, true, true, PhaseAwaitingObservation},
		{"issued", SettleModeObservation, false, false, false, PhaseAwaitingObservation},
		{"issued", SettleModeNone, false, false, true, PhaseQueued},
		{"issued", SettleModeNone, true, false, true, PhaseDelivered},
		{"issued", SettleModeNone, false, false, false, PhaseQueued},
		{"settled", SettleModeReply, true, true, true, "settled"},
		{"failed", SettleModeReply, true, true, true, "failed"},
		{"timed-out", SettleModeReply, false, false, true, "timed-out"},
	} {
		if got := CommandPhase(tc.status, tc.mode, tc.delivered, tc.executed, tc.deliverable); got != tc.want {
			t.Errorf("CommandPhase(%+v) = %q, want %q", tc, got, tc.want)
		}
	}
}

// A command settles from its reply only where its binding says so; a
// command that sets a value otherwise settles on observation, and one that
// sets nothing settles not at all.
func TestSettleModeOf(t *testing.T) {
	t.Parallel()
	reply := &driver.CommandBinding{Settle: &driver.Settle{From: driver.SettleFromResponse}}
	observe := &driver.CommandBinding{Settle: &driver.Settle{From: driver.SettleFromObserve}}
	unsaid := &driver.CommandBinding{}
	for _, tc := range []struct {
		b          *driver.CommandBinding
		settleable bool
		want       string
	}{
		{reply, true, SettleModeReply},
		{observe, true, SettleModeObservation},
		{unsaid, true, SettleModeObservation},
		{nil, true, SettleModeObservation},
		{reply, false, SettleModeNone},
		{nil, false, SettleModeNone},
	} {
		if got := SettleModeOf(tc.b, tc.settleable); got != tc.want {
			t.Errorf("SettleModeOf(%+v, %v) = %q, want %q", tc.b, tc.settleable, got, tc.want)
		}
	}
}

// A timeout says where the command stopped, naming the node, the endpoint or
// the type as the facts allow.
func TestTimeoutWords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                         string
		delivered, executed, carried bool
		execErr, endpoint, node      string
		want                         []string
	}{
		{"answered, nothing observed", true, true, true, "", "tcp", "edge", []string{"answered", "video-input", "15s"}},
		{"failed execution, nothing observed", true, true, true, "render: no arg", "tcp", "edge", []string{"execution failed", "render: no arg"}},
		{"delivered, no report", true, false, true, "", "tcp", "edge", []string{"delivered to node edge", "no execution report"}},
		{"delivered, node gone", true, false, false, "", "", "", []string{"delivered", "no execution report"}},
		{"queued, node never took it", false, false, true, "", "tcp", "edge", []string{"queued for node edge", "did not take it"}},
		{"queued, endpoint on no node", false, false, true, "", "tcp", "", []string{"endpoint tcp", "no node"}},
		{"carried by nothing", false, false, false, "", "", "", []string{"binds set-input", "no observation of video-input"}},
	} {
		got := timeoutWords("set-input", "video-input", "15s", tc.delivered, tc.executed, tc.carried, tc.execErr, tc.endpoint, tc.node)
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q, want it to mention %q", tc.name, got, w)
			}
		}
	}
}

// A fire-and-forget command that times out says where it stopped within its
// delivery window, and never mentions an observation: nothing was told to
// observe.
func TestDeliveryTimeoutWords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		delivered, carried bool
		endpoint, node     string
		want               string
	}{
		{"queued, node never took it", false, true, "tcp", "edge",
			"queued for node edge, which did not take it within its 10 minute delivery window"},
		{"delivered, no report", true, true, "tcp", "edge",
			"delivered to node edge, which sent no execution report within its 10 minute delivery window"},
		{"delivered, node since unplaced", true, true, "tcp", "",
			"delivered, but no execution report arrived within its 10 minute delivery window"},
		{"queued, endpoint on no node", false, true, "tcp", "",
			"queued on endpoint tcp, which is placed on no node"},
		{"carried by nothing", false, false, "", "",
			"no endpoint of this component has a driver that binds reboot"},
	} {
		got := deliveryTimeoutWords("reboot", tc.delivered, tc.carried, tc.endpoint, tc.node)
		if got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
		if strings.Contains(got, "observ") {
			t.Errorf("%s: %q mentions an observation", tc.name, got)
		}
	}
}

// A fire-and-forget command's report is its outcome (#1630): a clean report
// settles it, an execution error fails it with that error, both by the reply.
func TestReportOutcome(t *testing.T) {
	t.Parallel()
	if o := reportOutcome(""); o.status != "settled" || o.by != SettledByReply || o.reason != "the node carried it out" {
		t.Errorf("clean report = %+v, want settled by reply, carried out", o)
	}
	if o := reportOutcome("connection refused"); o.status != "failed" || o.by != SettledByReply || o.reason != "the execution failed: connection refused" {
		t.Errorf("error report = %+v, want failed by reply naming the error", o)
	}
}

// Stored evidence is valid text with no NUL, cut on a rune boundary at the
// cap and marked.
func TestStoredText(t *testing.T) {
	t.Parallel()
	if got := storedText("INPUT hdmi3"); got != "INPUT hdmi3" {
		t.Fatalf("short text = %q, want it whole", got)
	}
	if got := storedText("a\x00b\xffc"); strings.ContainsRune(got, 0) || !utf8.ValidString(got) {
		t.Fatalf("got %q, want valid text with no NUL", got)
	}
	long := strings.Repeat("é", ReplyCap)
	got := storedText(long)
	if !utf8.ValidString(got) || !strings.HasSuffix(got, " [truncated]") || len(got) > ReplyCap+len(" [truncated]") {
		t.Fatalf("long text cut to %d bytes (valid %v), want at most the cap, marked", len(got), utf8.ValidString(got))
	}
}
