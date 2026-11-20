package lab

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/settings"
)

// The lab's server plays a recorded omnibot model (#2121), so a run on the
// server, from the console or a demo, works with no live provider: the seed
// writes the recording into the data volume, and the server is pointed at it.
// A lab ships several recordings, one per use, by name (#2298): default
// reads the dev fleet's meeting 405, and corporate-outage triages the outage
// a marketing capture stages on the corporate fleet.

//go:embed omnibot/*.yaml
var omnibotRecordings embed.FS

// DefaultOmnibotReplay is the recording a lab plays unless the run names
// another.
const DefaultOmnibotReplay = "default"

// OmnibotReplayPath is where the lab's server reads its omnibot recording.
const OmnibotReplayPath = dataDir + "/omnibot-replay.yaml"

// OmnibotReplays is every recording a lab can play, by name, sorted.
func OmnibotReplays() []string {
	entries, _ := omnibotRecordings.ReadDir("omnibot")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), path.Ext(e.Name())))
	}
	slices.Sort(names)
	return names
}

// CheckOmnibotReplay refuses a recording the lab does not ship, naming the
// ones it does.
func CheckOmnibotReplay(name string) error {
	if slices.Contains(OmnibotReplays(), name) {
		return nil
	}
	return fmt.Errorf("no omnibot recording %q; omnibot recordings: %s", name, strings.Join(OmnibotReplays(), ", "))
}

// WriteOmnibotReplay writes the recording name into the data directory dir,
// as the server will find it.
func WriteOmnibotReplay(dir, name string) error {
	if err := CheckOmnibotReplay(name); err != nil {
		return err
	}
	b, err := omnibotRecordings.ReadFile("omnibot/" + name + ".yaml")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filepath.Base(OmnibotReplayPath)), b, 0o644)
}

// A lab run with --omnibot-key-env runs omnibot on a live model (#2109): the
// host's key reaches the server as the env-backed platform secret
// OmnibotKeySecret (#2223), declared by OmnibotKeySecretEnv, and the assistant
// settings name it from the env layer. Nothing is sealed, so a run without
// the flag simply does not declare it and the recording plays.
const (
	OmnibotKeySecret    = "lab-omnibot-key"
	OmnibotKeySecretEnv = secret.EnvPrefix + "LAB_OMNIBOT_KEY"
)

// OmnibotKeyServerEnv is the server variables --omnibot-key-env stands for:
// the key as the env secret OmnibotKeySecret, and the assistant's provider and
// key_secret naming it, each setting only when the shell's own variables
// (shell) do not already set it, so an exported setting wins over the alias.
func OmnibotKeyServerEnv(key string, shell []string) []string {
	doc, _ := json.Marshal(struct {
		Type   string            `json:"type"`
		Fields map[string]string `json:"fields"`
	}{"api-key", map[string]string{"key": key}})
	out := []string{OmnibotKeySecretEnv + "=" + string(doc)}
	set := map[string]bool{}
	for _, e := range shell {
		name, _, _ := strings.Cut(e, "=")
		set[name] = true
	}
	for _, kv := range [][2]string{
		{settings.EnvPrefix + "ASSISTANT__PROVIDER", "openai"},
		{settings.EnvPrefix + "ASSISTANT__KEY_SECRET", OmnibotKeySecret},
	} {
		if !set[kv[0]] {
			out = append(out, kv[0]+"="+kv[1])
		}
	}
	return out
}
