package lab

import (
	"context"
	"os"
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
// server works with no live provider; a scale lab, whose seed writes none,
// is not pointed at one, or its server would refuse to start.
func TestLabServerPlaysTheOmnibotRecording(t *testing.T) {
	t.Parallel()
	if got := serverEnv(t, testOptions())["OMNIGLASS_OMNIBOT_REPLAY"]; got != OmnibotReplayPath {
		t.Errorf("lab server OMNIGLASS_OMNIBOT_REPLAY = %q, want %q", got, OmnibotReplayPath)
	}
	if got, ok := serverEnv(t, scaleOptions())["OMNIGLASS_OMNIBOT_REPLAY"]; ok {
		t.Errorf("scale lab server OMNIGLASS_OMNIBOT_REPLAY = %q, want unset", got)
	}
}

// The seed writes the recording where the server reads it, and the server
// can play what it wrote.
func TestWriteOmnibotReplayWritesAPlayableRecording(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteOmnibotReplay(dir); err != nil {
		t.Fatalf("write: %v", err)
	}
	if filepath.Join(dataDir, filepath.Base(OmnibotReplayPath)) != OmnibotReplayPath {
		t.Fatalf("OmnibotReplayPath %q is not in the data directory", OmnibotReplayPath)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.Base(OmnibotReplayPath))); err != nil {
		t.Fatalf("the seed wrote no recording: %v", err)
	}
	if _, err := provider.LoadReplay(filepath.Join(dir, filepath.Base(OmnibotReplayPath))); err != nil {
		t.Fatalf("the server would refuse the recording: %v", err)
	}
}

// The recording plays the triage conversation to the triage-alarm skill's own
// prompt, calling only tools the skill names and ending on an assessment, and
// the tag conversation to any other prompt (#2163).
func TestOmnibotReplayTriagesTheTriageSkill(t *testing.T) {
	t.Parallel()
	r, err := provider.ParseReplay(omnibotReplay)
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
