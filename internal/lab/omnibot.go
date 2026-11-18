package lab

import (
	_ "embed"
	"os"
	"path/filepath"
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
