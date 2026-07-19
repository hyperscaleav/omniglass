// Package node is the edge runtime (omniglass node): it claims its identity in
// exchange for a NATS credential, connects outbound-only to the server's bus,
// pulls its worklist, and heartbeats. Checkpoint 2 stops at that round trip;
// running tasks (probes) and shipping telemetry are later checkpoints.
package node

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Credentials is the NATS credential a node receives from the claim exchange.
type Credentials struct {
	NatsURL  string `json:"nats_url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Claim posts the enrollment token to the server's public claim endpoint and
// returns the node's NATS credential. A non-200 (e.g. a bad token) is an error.
func Claim(ctx context.Context, serverURL, name, token string) (Credentials, error) {
	body, err := json.Marshal(map[string]string{"name": name, "token": token})
	if err != nil {
		return Credentials{}, fmt.Errorf("node: encode claim: %w", err)
	}
	url := strings.TrimRight(serverURL, "/") + "/api/v1/nodes:claim"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Credentials{}, fmt.Errorf("node: build claim request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("node: claim to %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("node: claim rejected (status %d): check the node name and enrollment token", resp.StatusCode)
	}
	var creds Credentials
	if err := json.NewDecoder(resp.Body).Decode(&creds); err != nil {
		return Credentials{}, fmt.Errorf("node: decode claim: %w", err)
	}
	return creds, nil
}
