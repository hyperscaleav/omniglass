package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// A file already at the path is replaced by this run's report.
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

// A ready file a killed server left behind (SIGKILL runs no shutdown) is
// cleared before the boot does anything that can fail or take time, so a
// reader polling that path never takes a dead run's ports for this run's,
// during the boot or after a boot that fails.
func TestABootClearsAStaleReadyFileFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ready.json")
	if err := os.WriteFile(path, []byte(`{"addr":"127.0.0.1:41234","nats_addr":"127.0.0.1:41236","nats_url":"nats://127.0.0.1:41236"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMNIGLASS_READY_FILE", path)
	t.Setenv("OMNIGLASS_DSN", "postgres://og:og@127.0.0.1:1/og?sslmode=disable")
	if err := runServer(context.Background(), ""); err == nil {
		t.Fatal("a server with an unreachable database booted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		b, _ := os.ReadFile(path)
		t.Fatalf("a failed boot left the stale ready file %s (stat: %v)", b, err)
	}
}

// The CLI guide's example ready file carries exactly the keys the server
// writes, so the teaching copy cannot drift from the report.
func TestTheCLIGuideShowsTheReadyFileAsWritten(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("..", "..", "docs", "src", "content", "docs", "guides", "cli.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(guide), "### A server on ports it picks")
	if !ok {
		t.Fatal("the CLI guide has no section teaching the ready file")
	}
	_, block, ok := strings.Cut(section, "```json\n")
	block, _, closed := strings.Cut(block, "```")
	if !ok || !closed {
		t.Fatal("the ready file section shows no example")
	}
	var shown map[string]string
	if err := json.Unmarshal([]byte(block), &shown); err != nil {
		t.Fatalf("the guide's example %q is not a JSON object: %v", block, err)
	}
	b, _ := json.Marshal(readyReport{})
	var written map[string]string
	_ = json.Unmarshal(b, &written)
	if len(shown) != len(written) {
		t.Fatalf("the guide shows keys %v, the server writes %v", keys(shown), keys(written))
	}
	for k := range written {
		if _, ok := shown[k]; !ok {
			t.Fatalf("the guide shows keys %v, the server writes %v", keys(shown), keys(written))
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
