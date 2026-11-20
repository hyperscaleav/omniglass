package lab

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/assistant"
	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
	"github.com/hyperscaleav/omniglass/internal/mcp/skills"
)

func serverEnv(t *testing.T, o Options) map[string]string {
	t.Helper()
	s, err := Plan(o)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	env := map[string]string{}
	for _, kv := range s.Server.Env {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	return env
}

// A lab's server plays the lab's omnibot recording (#2121), so a run on the
// server works with no live provider; a scale lab plays one too (#2298), so a
// demo of a corporate-size fleet needs no model.
func TestLabServerPlaysTheOmnibotRecording(t *testing.T) {
	t.Parallel()
	for name, o := range map[string]Options{"seeded": testOptions(), "scale": scaleOptions()} {
		if got := serverEnv(t, o)["OMNIGLASS_OMNIBOT_REPLAY"]; got != OmnibotReplayPath {
			t.Errorf("%s lab server OMNIGLASS_OMNIBOT_REPLAY = %q, want %q", name, got, OmnibotReplayPath)
		}
	}
}

// The seed is told which recording to write: the default one, or the one the
// run names (#2298), on a seeded and a scale lab alike.
func TestLabSeedWritesTheNamedRecording(t *testing.T) {
	t.Parallel()
	for name, o := range map[string]Options{"seeded": testOptions(), "scale": scaleOptions()} {
		s, err := Plan(o)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(s.Seed.Cmd, "--omnibot-replay") || s.Seed.Cmd[slices.Index(s.Seed.Cmd, "--omnibot-replay")+1] != DefaultOmnibotReplay {
			t.Errorf("%s seed %v, want --omnibot-replay %s", name, s.Seed.Cmd, DefaultOmnibotReplay)
		}
		o.OmnibotReplay = "corporate-outage"
		if s, err = Plan(o); err != nil {
			t.Fatal(err)
		}
		if s.Seed.Cmd[slices.Index(s.Seed.Cmd, "--omnibot-replay")+1] != "corporate-outage" {
			t.Errorf("%s seed %v, want --omnibot-replay corporate-outage", name, s.Seed.Cmd)
		}
	}
	o := testOptions()
	o.OmnibotReplay = "no-such-recording"
	if _, err := Plan(o); err == nil || !strings.Contains(err.Error(), "corporate-outage, default") {
		t.Errorf("an unknown recording: err = %v, want one naming the recordings", err)
	}
}

// Every recording the lab ships is one the server can play, and the seed
// writes the one it is named where the server reads it; an unknown name is
// refused, naming the ones there are.
func TestWriteOmnibotReplayWritesAPlayableRecording(t *testing.T) {
	t.Parallel()
	if filepath.Join(dataDir, filepath.Base(OmnibotReplayPath)) != OmnibotReplayPath {
		t.Fatalf("OmnibotReplayPath %q is not in the data directory", OmnibotReplayPath)
	}
	names := OmnibotReplays()
	if !slices.Contains(names, DefaultOmnibotReplay) || !slices.Contains(names, "corporate-outage") {
		t.Fatalf("recordings %v, want default and corporate-outage", names)
	}
	for _, name := range names {
		dir := t.TempDir()
		if err := WriteOmnibotReplay(dir, name); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if _, err := provider.LoadReplay(filepath.Join(dir, filepath.Base(OmnibotReplayPath))); err != nil {
			t.Fatalf("the server would refuse recording %s: %v", name, err)
		}
	}
	if err := WriteOmnibotReplay(t.TempDir(), "no-such-recording"); err == nil || !strings.Contains(err.Error(), "corporate-outage, default") {
		t.Errorf("an unknown recording: err = %v, want one naming the recordings", err)
	}
}

// The recording plays the triage conversation to the triage-alarm skill's own
// prompt, calling only tools the skill names and ending on an assessment, and
// the tag conversation to any other prompt (#2163).
func TestOmnibotReplayTriagesTheTriageSkill(t *testing.T) {
	t.Parallel()
	r, err := provider.ParseReplay(mustRecording(t, DefaultOmnibotReplay))
	if err != nil {
		t.Fatal(err)
	}
	sk, ok := skills.Get("triage-alarm")
	if !ok {
		t.Fatal("no triage-alarm skill")
	}
	prompt, err := sk.Prompt(map[string]string{"component": "hq.west.level-4.meeting-405.$comp.videobar-1"})
	if err != nil {
		t.Fatal(err)
	}
	p := r.Positioned()
	msgs := []provider.Message{{Role: provider.RoleUser, Text: assistant.HandedOver(prompt, nil)}}
	for {
		turn, err := p.Next(context.Background(), msgs, nil)
		if err != nil {
			t.Fatalf("triage run: %v", err)
		}
		msgs = append(msgs, provider.Message{Role: provider.RoleAssistant, Text: turn.Text, ToolCalls: turn.ToolCalls})
		if len(turn.ToolCalls) == 0 {
			if !strings.Contains(turn.Text, "alarm") {
				t.Errorf("triage ends on %q, want an assessment of the alarm", turn.Text)
			}
			break
		}
		for _, c := range turn.ToolCalls {
			if !slices.Contains(sk.Tools, c.Name) {
				t.Errorf("triage calls %s, which the skill does not name", c.Name)
			}
			msgs = append(msgs, provider.Message{Role: provider.RoleTool, ToolCallID: c.ID, Text: "{}"})
		}
	}

	other, err := p.Next(context.Background(), []provider.Message{{Role: provider.RoleUser, Text: "tag the meeting 405 video bar AV-0415"}}, nil)
	if err != nil || len(other.ToolCalls) != 1 || other.ToolCalls[0].Name != "get-component" {
		t.Fatalf("another prompt plays %+v, %v, want the tag conversation's get-component", other, err)
	}
}

func mustRecording(t *testing.T, name string) []byte {
	t.Helper()
	b, err := omnibotRecordings.ReadFile("omnibot/" + name + ".yaml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The corporate outage recording (#2298) triages the display a marketing
// capture unplugs: to the triage-alarm skill's prompt it calls only the
// skill's tools and ends on an assessment of the alarm, and to any other
// prompt it reads the room's health first.
func TestOmnibotCorporateOutageTriagesTheDisplay(t *testing.T) {
	t.Parallel()
	r, err := provider.ParseReplay(mustRecording(t, "corporate-outage"))
	if err != nil {
		t.Fatal(err)
	}
	sk, ok := skills.Get("triage-alarm")
	if !ok {
		t.Fatal("no triage-alarm skill")
	}
	const display = "hq.hq-annex.hq-annex-1.hq-annex-1-05.$comp.display-1"
	prompt, err := sk.Prompt(map[string]string{"component": display})
	if err != nil {
		t.Fatal(err)
	}
	p := r.Positioned()
	msgs := []provider.Message{{Role: provider.RoleUser, Text: assistant.HandedOver(prompt, nil)}}
	for {
		turn, err := p.Next(context.Background(), msgs, nil)
		if err != nil {
			t.Fatalf("triage run: %v", err)
		}
		msgs = append(msgs, provider.Message{Role: provider.RoleAssistant, Text: turn.Text, ToolCalls: turn.ToolCalls})
		if len(turn.ToolCalls) == 0 {
			if !strings.Contains(turn.Text, "alarm") {
				t.Errorf("triage ends on %q, want an assessment of the alarm", turn.Text)
			}
			break
		}
		for _, c := range turn.ToolCalls {
			if !slices.Contains(sk.Tools, c.Name) {
				t.Errorf("triage calls %s, which the skill does not name", c.Name)
			}
			msgs = append(msgs, provider.Message{Role: provider.RoleTool, ToolCallID: c.ID, Text: "{}"})
		}
	}
	other, err := p.Next(context.Background(), []provider.Message{{Role: provider.RoleUser, Text: "Why is this room down?"}}, nil)
	if err != nil || len(other.ToolCalls) != 1 || other.ToolCalls[0].Name != "get-system-health" {
		t.Fatalf("another prompt plays %+v, %v, want the room's get-system-health", other, err)
	}
}
