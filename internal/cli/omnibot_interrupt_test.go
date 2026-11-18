package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// syncBuffer is a writer the test reads while the command still writes to it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Interrupting omnibot while it waits on the person (Ctrl-C at Run it? [y/N])
// ends the run and still revokes its token: the question gives up on the
// cancelled run instead of blocking on input, and the revoke does not ride the
// cancelled context. Skipped under -short.
func TestOmnibotInterruptedStillRevokesItsToken(t *testing.T) {
	handler, tok := omnibotTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	ctx := context.Background()

	recording := filepath.Join(t.TempDir(), "lobby.yaml")
	if err := os.WriteFile(recording, []byte(`
turns:
  - tool_calls: [{name: create-component, arguments: {name: never-made, product: generic-device}}]
  - text: done
`), 0o600); err != nil {
		t.Fatal(err)
	}

	// The person never answers: stdin stays open and empty.
	stdin, unanswered := io.Pipe()
	defer unanswered.Close()
	var stderr syncBuffer
	root := Root("test")
	root.SetIn(stdin)
	root.SetOut(io.Discard)
	root.SetErr(&stderr)
	root.SetArgs([]string{"omnibot", "--server", srv.URL, "--token", tok, "--replay", recording, "make a component"})
	run, interrupt := context.WithCancel(ctx)
	defer interrupt()
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(run) }()

	deadline := time.Now().Add(30 * time.Second)
	for !strings.Contains(stderr.String(), "Run it?") {
		if time.Now().After(deadline) {
			t.Fatalf("omnibot never asked; stderr: %s", stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := omnibotTokens(t, srv.URL, tok); n != 1 {
		t.Fatalf("open omnibot tokens while it asks = %d, want 1", n)
	}
	interrupt()
	select {
	case err := <-done:
		if err == nil {
			t.Error("an interrupted run reported success")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("omnibot kept waiting for an answer after the run was interrupted")
	}
	if n := omnibotTokens(t, srv.URL, tok); n != 0 {
		t.Errorf("open omnibot tokens after the interrupt = %d, want 0 (stderr: %s)", n, stderr.String())
	}
}

// Interrupting omnibot while it is still connecting (its token minted, the
// MCP handshake not yet answered) revokes the token it just minted. Skipped
// under -short.
func TestOmnibotInterruptedWhileConnectingRevokesItsToken(t *testing.T) {
	handler, tok := omnibotTestHandler(t)
	dialing, released := make(chan struct{}, 1), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/mcp") {
			select {
			case dialing <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
			case <-released:
			}
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	defer close(released)

	recording := filepath.Join(t.TempDir(), "unplayed.yaml")
	if err := os.WriteFile(recording, []byte("turns:\n  - text: done\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := Root("test")
	root.SetIn(strings.NewReader(""))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"omnibot", "--server", srv.URL, "--token", tok, "--replay", recording, "anything"})
	run, interrupt := context.WithCancel(context.Background())
	defer interrupt()
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(run) }()

	select {
	case <-dialing:
	case err := <-done:
		t.Fatalf("omnibot ended before it dialed: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("omnibot never dialed the MCP endpoint")
	}
	interrupt()
	select {
	case err := <-done:
		if err == nil {
			t.Error("an interrupted connect reported success")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("omnibot kept connecting after the run was interrupted")
	}
	if n := omnibotTokens(t, srv.URL, tok); n != 0 {
		t.Errorf("open omnibot tokens after an interrupted connect = %d, want 0", n)
	}
}

// omnibotTestHandler is a real server over a seeded database, and its owner's
// token.
func omnibotTestHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("gw: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	future := time.Now().Add(time.Hour)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix, ExpiresAt: &future}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	return api.NewHandler(gw), tok
}

func omnibotTokens(t *testing.T, server, tok string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, server+"/api/v1/auth/me/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Sessions []struct {
			Description string `json:"description"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range out.Sessions {
		if s.Description == "omnibot session" {
			n++
		}
	}
	return n
}
