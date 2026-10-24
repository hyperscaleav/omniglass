package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The ready file names what the server bound, under the keys of the settings
// that asked for it, and nothing beside it is left in the directory: it is
// written to a temporary name and renamed into place, so a reader polling for
// it never reads half a file.
func TestTheReadyFileHoldsTheBoundAddresses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ready.json")
	want := readyReport{Addr: "127.0.0.1:41234", NatsAddr: "127.0.0.1:41236", NatsURL: "nats://127.0.0.1:41236"}
	remove, err := writeReadyFile(path, want)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]string
	if err := json.Unmarshal(b, &keys); err != nil {
		t.Fatalf("ready file %q is not a JSON object: %v", b, err)
	}
	for k, v := range map[string]string{"addr": want.Addr, "nats_addr": want.NatsAddr, "nats_url": want.NatsURL} {
		if keys[k] != v {
			t.Errorf("ready file %s = %q, want %q (all: %s)", k, keys[k], v, b)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the directory holds %d entries after the write, want the ready file alone", len(entries))
	}

	remove()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the ready file outlived its server's shutdown (stat: %v)", err)
	}
}

// A ready file left by an earlier run is replaced, never read as this run's.
func TestTheReadyFileReplacesAStaleOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ready.json")
	if err := os.WriteFile(path, []byte(`{"addr":"127.0.0.1:1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := writeReadyFile(path, readyReport{Addr: "127.0.0.1:2"}); err != nil {
		t.Fatal(err)
	}
	var got readyReport
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &got); err != nil || got.Addr != "127.0.0.1:2" {
		t.Fatalf("ready file = %s (%v), want this run's address", b, err)
	}
}

// No path, no file: a server nobody asked to report runs as it always did.
func TestNoReadyFileWithoutAPath(t *testing.T) {
	remove, err := writeReadyFile("", readyReport{Addr: "127.0.0.1:2"})
	if err != nil {
		t.Fatal(err)
	}
	remove()
}

// A path the server cannot write is a boot error, not a silent skip: whoever
// set it is waiting on the file.
func TestAnUnwritableReadyFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "ready.json")
	if _, err := writeReadyFile(path, readyReport{Addr: "127.0.0.1:2"}); err == nil {
		t.Fatal("a ready file in a missing directory was accepted")
	}
}
