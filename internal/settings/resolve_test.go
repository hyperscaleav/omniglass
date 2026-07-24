package settings

import "testing"

func defaultLevel() Level {
	return Level{Name: "default", Doc: Doc{"ui": {"theme": "omniglass-dark", "default_landing": "/"}}}
}

func TestResolveMostSpecificWins(t *testing.T) {
	r := Resolve(
		defaultLevel(),
		Level{Name: "file", Doc: Doc{"ui": {"theme": "omniglass-light"}}},
		Level{Name: "platform", Doc: Doc{"ui": {"default_landing": "/alarms"}}},
	)
	if r.Values["ui"]["theme"] != "omniglass-light" {
		t.Fatalf("theme = %v, want omniglass-light (file over default)", r.Values["ui"]["theme"])
	}
	if r.Values["ui"]["default_landing"] != "/alarms" {
		t.Fatalf("landing = %v, want /alarms (platform over default)", r.Values["ui"]["default_landing"])
	}
	if r.Sources["ui.theme"] != "file" {
		t.Fatalf("theme source = %v, want file", r.Sources["ui.theme"])
	}
	if r.Sources["ui.default_landing"] != "platform" {
		t.Fatalf("landing source = %v, want platform", r.Sources["ui.default_landing"])
	}
}

func TestResolveLockPinsBroaderValue(t *testing.T) {
	// platform sets theme AND locks it; a (hypothetical) more-specific level cannot win.
	r := Resolve(
		defaultLevel(),
		Level{
			Name:  "platform",
			Doc:   Doc{"ui": {"theme": "omniglass-dark"}},
			Locks: map[string][]string{"ui": {"theme"}},
		},
		Level{Name: "user", Doc: Doc{"ui": {"theme": "omniglass-light"}}},
	)
	if r.Values["ui"]["theme"] != "omniglass-dark" {
		t.Fatalf("locked theme = %v, want omniglass-dark (platform lock beats user)", r.Values["ui"]["theme"])
	}
	if r.Sources["ui.theme"] != "platform" {
		t.Fatalf("locked theme source = %v, want platform", r.Sources["ui.theme"])
	}
	if r.Locks["ui.theme"] != "platform" {
		t.Fatalf("theme lock level = %v, want platform", r.Locks["ui.theme"])
	}
}

// TestResolveNamesTheOffAxisDefault asserts an unoverridden key reports the
// declaration as its source, distinguishable from an admin's platform binding.
func TestResolveNamesTheOffAxisDefault(t *testing.T) {
	r := Resolve(
		Level{Name: "default", Doc: Doc{"ui": {"theme": "omniglass-dark", "density": "cozy"}}},
		Level{Name: "file", Doc: Doc{}},
		Level{Name: "platform", Doc: Doc{"ui": {"theme": "omniglass-light"}}},
	)
	if got := r.Sources["ui.theme"]; got != "platform" {
		t.Fatalf("ui.theme source = %q, want platform", got)
	}
	if got := r.Sources["ui.density"]; got != "default" {
		t.Fatalf("ui.density source = %q, want default", got)
	}
	if got := r.Values["ui"]["theme"]; got != "omniglass-light" {
		t.Fatalf("ui.theme = %v, want omniglass-light", got)
	}
}

func TestResolvePanicsOnDuplicateLevelNames(t *testing.T) {
	// Lock identity keys on the level name, so duplicate names would let a more-
	// specific level bypass a broader lock. Reject it as a programming defect.
	defer func() {
		if recover() == nil {
			t.Fatalf("Resolve did not panic on duplicate level names")
		}
	}()
	Resolve(
		Level{Name: "dup", Doc: Doc{"ui": {"theme": "a"}}},
		Level{Name: "dup", Doc: Doc{"ui": {"theme": "b"}}},
	)
}
