package labfootprint

import (
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
	tt, ss, err := pickDrivers(targets, skips, []string{"http-json", "ucc-webhook"})
	if err != nil || len(tt) != 1 || len(ss) != 1 {
		t.Fatalf("picked %v %v: %v", tt, ss, err)
	}
}
