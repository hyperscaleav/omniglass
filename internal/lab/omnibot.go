package lab

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/settings"
)

// The lab's server plays a recorded omnibot model (#2121), so a run on the
// server, from the console or a demo, works with no live provider: the seed
// writes the recording into the data volume, and the server is pointed at it.

//go:embed omnibot-replay.yaml
var omnibotReplay []byte

// OmnibotReplayPath is where the lab's server reads its omnibot recording.
const OmnibotReplayPath = dataDir + "/omnibot-replay.yaml"

// WriteOmnibotReplay writes the lab's omnibot recording into the data
// directory dir, as the server will find it.
func WriteOmnibotReplay(dir string) error {
	return os.WriteFile(filepath.Join(dir, filepath.Base(OmnibotReplayPath)), omnibotReplay, 0o644)
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
