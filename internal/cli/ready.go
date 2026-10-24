package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// readyReport is what the ready file says once the server serves: the
// address each listener bound, under the key of the setting that asked for
// it, so a listen address naming port 0 reads back as the port the kernel
// handed out, and the bus URL the node-claim exchange advertises.
type readyReport struct {
	Addr     string `json:"addr"`
	NatsAddr string `json:"nats_addr"`
	NatsURL  string `json:"nats_url"`
}

// writeReadyFile writes r to path (OMNIGLASS_READY_FILE) whole or not at
// all: to a temporary name beside it, then renamed into place, so a reader
// polling for the file never reads half of one. The returned remove deletes
// it again, for the server's shutdown, so the file names a live server. An
// empty path writes nothing.
func writeReadyFile(path string, r readyReport) (remove func(), err error) {
	if path == "" {
		return func() {}, nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("ready file: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return nil, fmt.Errorf("ready file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("ready file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return nil, fmt.Errorf("ready file: %w", err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return nil, fmt.Errorf("ready file: %w", err)
	}
	return func() { _ = os.Remove(path) }, nil
}
