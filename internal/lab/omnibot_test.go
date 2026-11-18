package lab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
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
