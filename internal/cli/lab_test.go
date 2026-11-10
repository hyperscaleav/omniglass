package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/lab"
)

// The script-only flags of lab run (#1197) exist for the demo pipeline's
// recorders and the probe, and stay out of a learner's help.
func TestLabRunScriptFlagsAreHidden(t *testing.T) {
	cmd := newLabRunCmd("test")
	for _, name := range []string{"project", "ephemeral", "ready-file", "while-pid", "no-node"} {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("lab run has no --%s", name)
			continue
		}
		if !f.Hidden {
			t.Errorf("--%s shows in a learner's help", name)
		}
	}
	if strings.Contains(cmd.UsageString(), "ready-file") {
		t.Error("the usage names --ready-file")
	}
}

// Every walkthrough's setup, as the lab guide lists it for a learner, is a
// command this CLI has, with every flag it names: the guide never teaches a
// command that does not run.
func TestWalkthroughSetupCommandsAreRealCommands(t *testing.T) {
	b, err := lab.FactsJSON(os.DirFS("../../web/e2e/walkthroughs"))
	if err != nil {
		t.Fatal(err)
	}
	var facts lab.Facts
	if err := json.Unmarshal(b, &facts); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, w := range facts.Walkthroughs {
		for _, line := range w.Setup {
			n++
			words := strings.Fields(line)
			cmd, rest, err := newRoot("test").Find(words[1:])
			if err != nil || cmd.Name() != "create" {
				t.Errorf("%s: %q is not a command: %v", w.ID, line, err)
				continue
			}
			for _, word := range rest {
				if name, ok := strings.CutPrefix(word, "--"); ok && cmd.Flags().Lookup(name) == nil {
					t.Errorf("%s: %q names --%s, which %s lacks", w.ID, line, name, cmd.CommandPath())
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no walkthrough has a setup to check (set-up-monitoring stages bay-2-display)")
	}
}

// The lab's node runs a command this binary has, with every flag it names
// (#1273 added --probe-every to both): a learner's lab and a script's.
func TestLabNodeCommandIsARealCommand(t *testing.T) {
	for _, ephemeral := range []bool{false, true} {
		s, err := lab.Plan(lab.Options{Image: "omniglass:dev", Project: "check", Port: lab.DefaultPort,
			NodeToken: "tok", NodeTokenHash: "abc", Ephemeral: ephemeral})
		if err != nil {
			t.Fatal(err)
		}
		cmd, rest, err := newRoot("test").Find(s.Node.Cmd)
		if err != nil || cmd.CommandPath() != "omniglass node run" {
			t.Fatalf("lab node %v is not node run: %v", s.Node.Cmd, err)
		}
		for _, word := range rest {
			if name, ok := strings.CutPrefix(word, "--"); ok && cmd.Flags().Lookup(name) == nil {
				t.Errorf("the lab's node passes --%s, which node run lacks", name)
			}
		}
	}
}

// A scale lab scenario's --plan (#1553) prints the resolved plan and starts
// nothing: no Docker is needed, so this runs anywhere.
func TestLabRunScenarioPlan(t *testing.T) {
	for size, sites := range map[string]string{"100": "3", "1000": "18", "3000": "30"} {
		var out strings.Builder
		root := newRoot("test")
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"lab", "run", "--scenario", "corporate", "--systems", size, "--plan"})
		if err := root.Execute(); err != nil {
			t.Fatalf("--systems %s: %v", size, err)
		}
		for _, want := range []string{"scenario corporate (Corporate): " + size + " systems", "across " + sites + " sites",
			"ARCHETYPE", "regional-01", "huddle-room", "PRODUCT"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("--systems %s: the plan lacks %q:\n%s", size, want, out.String())
			}
		}
	}
}

// The forced host memory a scale lab plans against reads as binary sizes.
func TestParseByteSizeReadsBinarySuffixes(t *testing.T) {
	for in, want := range map[string]int64{
		"": 0, "1024": 1024, "2k": 2 << 10, "512m": 512 << 20, "2g": 2 << 30, "12G": 12 << 30,
	} {
		got, err := parseByteSize(in)
		if err != nil || got != want {
			t.Errorf("parseByteSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"banana", "-1g", "g", "2t"} {
		if _, err := parseByteSize(in); err == nil {
			t.Errorf("parseByteSize(%q) read as a size", in)
		}
	}
}

// A scale lab scenario without a size, or with a forced host memory that is
// no size, refuses with what to do before it starts anything; the size knob
// on a seeded fleet is refused too.
func TestLabRunScenarioRefusals(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--scenario", "corporate", "--systems", "100", "--host-memory", "banana"}, "--host-memory"},
		{[]string{"--scenario", "corporate", "--plan"}, "--systems N (3 to 10000)"},
		{[]string{"--scenario", "corporate", "--systems", "0", "--plan"}, "--systems N"},
		{[]string{"--scenario", "corporate", "--systems", "-3", "--plan"}, "out of range"},
		{[]string{"--scenario", "corporate", "--systems", "2", "--plan"}, "ask for 3 to 10000"},
		{[]string{"--scenario", "dev", "--systems", "100"}, `"dev" is a seeded fleet`},
		{[]string{"--scenario", "dev", "--keep"}, `--keep keeps a scale lab scenario's data`},
		{[]string{"--scenario", "corporate", "--systems", "20", "--keep", "--ephemeral"}, "--keep and --ephemeral"},
		{[]string{"--scenario", "nope", "--plan"}, "scale lab scenarios: corporate"},
	} {
		root := newRoot("test")
		var out strings.Builder
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(append([]string{"lab", "run"}, tc.args...))
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("lab run %v = %v, want an error containing %q", tc.args, err, tc.want)
		}
	}
}

// A scale lab's --plan ends with its preflight (#1554): the estimate, what
// the Docker host can spare, and the decision bring-up would make, a refusal
// naming the largest size that fits; without a Docker host, the estimate
// alone. It starts nothing either way.
func TestLabRunScenarioPlanPreflight(t *testing.T) {
	host := func(_ context.Context, forced int64) (lab.HostResources, error) {
		mem := int64(32 << 30)
		if forced > 0 {
			mem = forced
		}
		return lab.HostResources{MemBytes: mem, CPUs: 12}, nil
	}
	plan := func(t *testing.T, systems int, mem string, readHost hostReader) string {
		t.Helper()
		var out strings.Builder
		if err := runLab(context.Background(), &out, "test", labFlags{scenario: "corporate", systems: systems, plan: true,
			scale: labScale{hostMemory: mem}, readHost: readHost}); err != nil {
			t.Fatal(err)
		}
		return out.String()[strings.Index(out.String(), "\nPreflight"):]
	}
	need := lab.MeasuredCost.Estimate(18, 3016)
	for name, tc := range map[string]struct {
		systems  int
		mem      string
		readHost hostReader
		want     []string
	}{
		"fits": {1000, "", host, []string{
			fmt.Sprintf("Preflight: about %s of memory and %.1f cores (the server and Postgres, a node for each of 18 sites, 3016 simulated components)",
				lab.FormatMiB(need.MemMiB), need.CPU),
			"This Docker host can spare 25.6 GiB and 9.6 cores (80% of its 32.0 GiB and 12 cores)", "Decision: fits."}},
		"refused": {3000, "4g", host, []string{"Decision: refused. A scale lab of 3000 systems (30 sites, 8950 simulated components)",
			"this Docker host has 4.0 GiB and 12 cores", ", the largest that fits"}},
		"none fits": {100, "64m", host, []string{"Decision: refused.", "Not even the smallest size fits", "the smallest scale lab, --systems 3"}},
		"no docker": {100, "", func(context.Context, int64) (lab.HostResources, error) {
			return lab.HostResources{}, errors.New("no daemon")
		},
			[]string{"Preflight: about ", "No decision: no daemon"}},
	} {
		t.Run(name, func(t *testing.T) {
			out := plan(t, tc.systems, tc.mem, tc.readHost)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("the plan lacks %q:\n%s", want, out)
				}
			}
		})
	}
	// The size a refusal names is the largest that fits: it fits, and the
	// next does not.
	refusal := plan(t, 3000, "4g", host)
	var largest int
	if _, err := fmt.Sscanf(refusal[strings.Index(refusal, "run --systems "):], "run --systems %d,", &largest); err != nil {
		t.Fatalf("no size named: %v\n%s", err, refusal)
	}
	if out := plan(t, largest, "4g", host); !strings.Contains(out, "Decision: fits.") {
		t.Errorf("the named --systems %d does not fit:\n%s", largest, out)
	}
	if out := plan(t, largest+1, "4g", host); !strings.Contains(out, "Decision: refused.") {
		t.Errorf("--systems %d, one past the named size, fits:\n%s", largest+1, out)
	}
}

// --keep (#1633) makes a scale lab one that keeps its data: the run is not
// throwaway, and --fresh, which the scale path used to drop, reaches the run.
// Without --keep a scale lab stays throwaway. The run is captured, so no
// Docker is needed.
func TestLabRunScaleKeepReachesTheRunConfig(t *testing.T) {
	for _, tc := range []struct {
		keep, fresh   bool
		wantEphemeral bool
	}{
		{keep: false, fresh: false, wantEphemeral: true},
		{keep: true, fresh: true, wantEphemeral: false},
		{keep: true, fresh: false, wantEphemeral: false},
	} {
		var got lab.Config
		var out strings.Builder
		err := runLab(context.Background(), &out, "test", labFlags{scenario: "corporate", systems: 20, keep: tc.keep, fresh: tc.fresh,
			project: lab.DefaultProject, listen: lab.DefaultListen, port: lab.DefaultPort,
			start: func(_ context.Context, _ io.Writer, cfg lab.Config) error { got = cfg; return nil }})
		if err != nil {
			t.Fatalf("keep %v fresh %v: %v", tc.keep, tc.fresh, err)
		}
		if got.Ephemeral != tc.wantEphemeral || got.Fresh != tc.fresh {
			t.Errorf("keep %v fresh %v: run config Ephemeral %v Fresh %v, want Ephemeral %v Fresh %v",
				tc.keep, tc.fresh, got.Ephemeral, got.Fresh, tc.wantEphemeral, tc.fresh)
		}
		if got.Project != lab.ScaleProject || got.ScaleRun == nil || len(got.SiteNodes) == 0 {
			t.Errorf("keep %v: project %q, scale run %v, %d site nodes; want the scale lab's own project and its nodes",
				tc.keep, got.Project, got.ScaleRun != nil, len(got.SiteNodes))
		}
	}
}
