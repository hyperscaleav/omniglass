package labfootprint

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A sweep's lab is its own and holds nothing but what the sweep adds: no
// fleet, no node of its own, bound to the process that runs it.
func TestADriverSweepsLabIsEmptyAndItsOwn(t *testing.T) {
	args := driverRunArgs(DriverConfig{Project: "omniglass-sweep"}, "/x/lab.env", 42)
	got := strings.Join(args, " ")
	for _, want := range []string{"lab run", "--ephemeral", "--project omniglass-sweep", "--port 0",
		"--ready-file /x/lab.env", "--while-pid 42", "--scenario empty", "--no-node", "--cadence lab"} {
		if !strings.Contains(got, want) {
			t.Fatalf("args %q lack %q", got, want)
		}
	}
	if slices.Contains(args, "--image") {
		t.Fatalf("no image asked for, want the lab's default: %q", got)
	}
	args = driverRunArgs(DriverConfig{Project: "p", Image: "omniglass:x"}, "r", 1)
	if i := slices.Index(args, "--image"); i < 0 || args[i+1] != "omniglass:x" {
		t.Fatalf("args %v, want --image omniglass:x", args)
	}
}

func TestASweepCanBeNarrowedToNamedDrivers(t *testing.T) {
	targets := []DriverTarget{{Driver: "a"}, {Driver: "b"}, {Driver: "c"}}
	skips := []DriverSkip{{Driver: "s", Reason: "no product"}}

	tt, ss, err := pickDrivers(targets, skips, nil)
	if err != nil || len(tt) != 3 || len(ss) != 1 {
		t.Fatalf("no names keeps the whole plan: %v %v %v", tt, ss, err)
	}
	tt, ss, err = pickDrivers(targets, skips, []string{"c", "s"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tt) != 1 || tt[0].Driver != "c" || len(ss) != 1 || ss[0].Driver != "s" {
		t.Fatalf("picked %v and %v, want c measured and s still skipped with its reason", tt, ss)
	}
	if _, _, err := pickDrivers(targets, skips, []string{"a", "nope"}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("an unknown name is a typo, not an empty sweep: %v", err)
	}
}

// Each driver is measured idle, then at three loads: memory fits its slope
// over the loaded steps alone, so it needs three of them to show a bend.
func TestADriverIsMeasuredIdleThenAtThreeLoads(t *testing.T) {
	if got := stepComponents(200); !slices.Equal(got, []int{0, 200, 400, 600}) {
		t.Fatalf("steps = %v", got)
	}
}

func TestASweepsComponentsAreNamedUnderItsNode(t *testing.T) {
	if got := sweepComponentName("sweep-http-json", 7); got != "sweep-http-json-0007" {
		t.Fatalf("name = %q", got)
	}
}

// A sim secret is created as the create route takes it, its optional owner
// and sensitivity sent only when the spec sets them.
func TestASimSecretIsCreatedAsTheRouteTakesIt(t *testing.T) {
	owner, sensitive := "lyra", true
	b := secretBody(storage.SecretSpec{Name: "n", Label: "L", SecretType: "snmp-v3", OwnerKind: "vendor",
		OwnerName: &owner, AdminSensitive: &sensitive, Fields: map[string]string{"user": "u"}})
	if b["name"] != "n" || b["label"] != "L" || b["secret_type"] != "snmp-v3" || b["owner_kind"] != "vendor" ||
		b["owner"] != "lyra" || b["admin_sensitive"] != true {
		t.Fatalf("body = %v", b)
	}
	b = secretBody(storage.SecretSpec{Name: "n", SecretType: "basic", OwnerKind: "platform"})
	for _, k := range []string{"label", "owner", "admin_sensitive"} {
		if _, ok := b[k]; ok {
			t.Fatalf("an unset %s is left out, got %v", k, b)
		}
	}
}

// The poll CPU is read after two report windows, so the window that held a
// step's last run has closed and landed.
func TestAStepsPollCPUIsReadAfterItsWindowLands(t *testing.T) {
	if got := reportSettle(10 * time.Second); got != 20*time.Second {
		t.Fatalf("settle = %s, want two of the lab's 10s report windows", got)
	}
}

// The report window is the lab's live setting, not the platform default: a
// lab that runs a shorter one lands its poll CPU sooner, and one that runs
// a longer one later.
func TestAReportWindowIsTheLabsLiveSetting(t *testing.T) {
	got, err := reportWindowOf([]byte(`{"values":{"collection":{"report_seconds":10}}}`))
	if err != nil || got != 10*time.Second {
		t.Fatalf("report window = %s, %v, want 10s", got, err)
	}
	for _, body := range []string{`{"values":{}}`, `{"values":{"collection":{"report_seconds":0}}}`, `not json`} {
		if _, err := reportWindowOf([]byte(body)); err == nil {
			t.Fatalf("%s: want an error, not a guessed window", body)
		}
	}
}

// The shipped plan is the default, so a sweep that names nothing measures
// every driver with a product and lists the rest.
func TestTheShippedPlanNamesEveryDriverOnce(t *testing.T) {
	targets, skips := ShippedDriverSweep()
	tt, ss, err := pickDrivers(targets, skips, []string{"tessellate-census-http", "ucc-webhook"})
	if err != nil || len(tt) != 1 || len(ss) != 1 {
		t.Fatalf("picked %v %v: %v", tt, ss, err)
	}
}

// A driver whose memory does not show at a step is swept again at four
// times it, up to the cap, and the last step short of the cap is the cap
// itself: a cheap driver's few KiB a task is inside the noise at 400 tasks
// and a line at a few thousand.
func TestAStepTooSmallToSeeMemoryGrowsToTheCap(t *testing.T) {
	for _, c := range []struct{ step, cap, want int }{
		{400, 6400, 1600}, {1600, 6400, 6400}, {2000, 6400, 6400}, {6400, 6400, 0}, {400, 400, 0}, {400, 0, 0},
	} {
		got, ok := nextStepTasks(c.step, c.cap)
		if got != c.want || ok != (c.want > 0) {
			t.Fatalf("nextStepTasks(%d, %d) = %d, %v; want %d", c.step, c.cap, got, ok, c.want)
		}
	}
}

// Only a sweep whose memory did not fit is taken again: one that measured
// it is done, and one that failed for any other reason (the node never
// came up) would fail the same way at a bigger step.
func TestOnlyAMemoryMissIsSweptAgain(t *testing.T) {
	for _, c := range []struct {
		name string
		a    driverAttempt
		want bool
	}{
		{"measured", driverAttempt{row: DriverCost{Driver: "d"}}, false},
		{"memory missed", driverAttempt{row: DriverCost{MemoryNotMeasured: "not a line"}}, true},
		{"neither fit", driverAttempt{err: noFitError("d memory: x; d cpu: y")}, true},
		{"broke", driverAttempt{err: errors.New("start the node: no")}, false},
		{"worklist undelivered", driverAttempt{err: worklistUndeliveredError{installed: 16000, held: 24000}}, false},
	} {
		if got := c.a.wantsBiggerStep(); got != c.want {
			t.Fatalf("%s: wantsBiggerStep = %v, want %v", c.name, got, c.want)
		}
	}
}

// Of a driver's attempts the one kept measured the most figures, and of
// those the biggest step: a bigger step that lost the CPU fit does not
// replace a smaller one that had it, and one that broke keeps what came
// before it.
func TestADriversKeptSweepIsItsMostMeasured(t *testing.T) {
	small := DriverCost{StepTasks: 400, MemoryNotMeasured: "noise"}
	bigNoCPU := DriverCost{StepTasks: 1600, MemoryNotMeasured: "noise", CPUNotMeasured: "bent"}
	big := DriverCost{StepTasks: 6400}
	got, err := keptAttempt([]driverAttempt{{row: small}, {row: bigNoCPU}})
	if err != nil || got.StepTasks != 400 {
		t.Fatalf("kept %+v, %v; want the 400 step, which had cpu", got, err)
	}
	got, err = keptAttempt([]driverAttempt{{row: small}, {row: bigNoCPU}, {row: big}})
	if err != nil || got.StepTasks != 6400 {
		t.Fatalf("kept %+v, %v; want the 6400 step, which had both", got, err)
	}
	got, err = keptAttempt([]driverAttempt{{row: small}, {err: errors.New("broke")}})
	if err != nil || got.StepTasks != 400 {
		t.Fatalf("kept %+v, %v; want the 400 step over the broken one", got, err)
	}
	if _, err = keptAttempt([]driverAttempt{{err: noFitError("x")}, {err: errors.New("broke")}}); err == nil || err.Error() != "broke" {
		t.Fatalf("no row at all: %v, want the last error", err)
	}
}

// A step whose worklist the node never installed measured a node running
// an older one (#1586): the server refuses a reply over the bus's payload
// cap and the node keeps what it had. The sweep halves the step, never back
// to one already swept, until the worklist arrives; and a step that missed
// memory below the refused one grows no further than half of it.
func TestAWorklistTheNodeNeverTookHalvesTheStep(t *testing.T) {
	refused := driverAttempt{err: worklistUndeliveredError{installed: 16000, held: 24000}}
	missed := driverAttempt{row: DriverCost{MemoryNotMeasured: "not a line"}}
	measured := driverAttempt{row: DriverCost{Driver: "d"}}
	for _, c := range []struct {
		name                string
		a                   driverAttempt
		step, prev, maxStep int
		want, wantMax       int
	}{
		{"refused after a miss", refused, 6400, 1600, 6400, 3200, 3200},
		{"refused first", refused, 6400, 0, 6400, 3200, 3200},
		{"refused down to the step before", refused, 3200, 1600, 3200, 0, 3200},
		{"a miss under a refusal's cap", missed, 1600, 0, 3200, 3200, 3200},
		{"a miss at the cap", missed, 3200, 1600, 3200, 0, 3200},
		{"a miss grows", missed, 1600, 0, 6400, 6400, 6400},
		{"measured", measured, 1600, 0, 6400, 0, 6400},
	} {
		got, gotMax, ok := nextAttempt(c.a, c.step, c.prev, c.maxStep)
		if got != c.want || ok != (c.want > 0) || (ok && gotMax != c.wantMax) {
			t.Fatalf("%s: nextAttempt = %d, %d, %v; want %d, %d", c.name, got, gotMax, ok, c.want, c.wantMax)
		}
	}
}

func TestAnUndeliveredWorklistSaysWhatTheNodeRuns(t *testing.T) {
	got := worklistUndeliveredError{installed: 16000, held: 24000}.Error()
	for _, want := range []string{"16000", "24000", "max payload", "#1586"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q lacks %q", got, want)
		}
	}
}
