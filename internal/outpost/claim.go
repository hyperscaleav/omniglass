// Package node is the edge runtime (omniglass node): it claims its identity in
// exchange for a NATS credential, connects outbound-only to the server's bus,
// pulls its worklist, and heartbeats. Checkpoint 2 stops at that round trip;
// running tasks (probes) and shipping telemetry are later checkpoints.
package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/redact"
)

// Credentials is the NATS credential a node receives from the claim exchange.
type Credentials struct {
	NatsURL  string `json:"nats_url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ErrClaimRejected is a claim the server answered and refused (a wrong name
// or token), as opposed to one that could not reach it.
var ErrClaimRejected = errors.New("node: claim rejected")

// Claim posts the enrollment token to the server's public claim endpoint and
// returns the node's NATS credential. A non-200 (e.g. a bad token) is an error.
func Claim(ctx context.Context, serverURL, name, token string) (Credentials, error) {
	body, err := json.Marshal(map[string]string{"name": name, "token": token})
	if err != nil {
		return Credentials{}, fmt.Errorf("node: encode claim: %w", err)
	}
	// The server URL can carry a user:password, so no error below names the
	// raw URL: redact checks the base without quoting it (refusing a password
	// misread into the path, which nothing would mask), and net/http's own
	// transport error names the URL with the password masked. sim run logs a
	// failed claim on every retry, so a raw URL here reached the logs.
	base := strings.TrimRight(serverURL, "/")
	if _, err := redact.ParseBaseURL(base, "the server URL (--server or $OMNIGLASS_SERVER)"); err != nil {
		return Credentials{}, fmt.Errorf("node: claim: %w", err)
	}
	url := base + "/api/v1/nodes:claim"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Credentials{}, fmt.Errorf("node: build claim request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("node: claim: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("%w (status %d): check the node name and enrollment token", ErrClaimRejected, resp.StatusCode)
	}
	var creds Credentials
	if err := json.NewDecoder(resp.Body).Decode(&creds); err != nil {
		return Credentials{}, fmt.Errorf("node: decode claim: %w", err)
	}
	return creds, nil
}
