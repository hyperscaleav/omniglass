package settings

import "testing"

func codeLevel() Level {
	return Level{Name: "code", Doc: Doc{"ui": {"theme": "omniglass-dark", "default_landing": "/"}}}
}

func TestResolveMostSpecificWins(t *testing.T) {
	r := Resolve(
		codeLevel(),
		Level{Name: "file", Doc: Doc{"ui": {"theme": "omniglass-light"}}},
		Level{Name: "global", Doc: Doc{"ui": {"default_landing": "/alarms"}}},
	)
	if r.Values["ui"]["theme"] != "omniglass-light" {
		t.Fatalf("theme = %v, want omniglass-light (file over code)", r.Values["ui"]["theme"])
	}
	if r.Values["ui"]["default_landing"] != "/alarms" {
		t.Fatalf("landing = %v, want /alarms (global over code)", r.Values["ui"]["default_landing"])
	}
	if r.Sources["ui.theme"] != "file" {
		t.Fatalf("theme source = %v, want file", r.Sources["ui.theme"])
	}
	if r.Sources["ui.default_landing"] != "global" {
		t.Fatalf("landing source = %v, want global", r.Sources["ui.default_landing"])
	}
}

func TestResolveLockPinsBroaderValue(t *testing.T) {
	// global sets theme AND locks it; a (hypothetical) more-specific level cannot win.
	r := Resolve(
		codeLevel(),
		Level{
			Name:  "global",
			Doc:   Doc{"ui": {"theme": "omniglass-dark"}},
			Locks: map[string][]string{"ui": {"theme"}},
		},
		Level{Name: "user", Doc: Doc{"ui": {"theme": "omniglass-light"}}},
	)
	if r.Values["ui"]["theme"] != "omniglass-dark" {
		t.Fatalf("locked theme = %v, want omniglass-dark (global lock beats user)", r.Values["ui"]["theme"])
	}
	if r.Sources["ui.theme"] != "global" {
		t.Fatalf("locked theme source = %v, want global", r.Sources["ui.theme"])
	}
	if r.Locks["ui.theme"] != "global" {
		t.Fatalf("theme lock level = %v, want global", r.Locks["ui.theme"])
	}
}
