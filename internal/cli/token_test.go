package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
)

// TestTokenTTLCap proves the token and bootstrap commands reject a --ttl above the
// hard maximum lifetime before touching the database (issue #172): every credential
// is time-bounded, and the cap is an absolute ceiling, so a request for a longer-lived
// token is a clear error, not a silent clamp.
func TestTokenTTLCap(t *testing.T) {
	ctx := context.Background()
	overCap := auth.MaxTokenLifetime + 24*time.Hour

	// omniglass token <user> --ttl <over cap> errors, and the message names the cap.
	err := runToken(ctx, "root", overCap)
	if err == nil {
		t.Fatalf("runToken with ttl above the cap should error, got nil")
	}
	if !strings.Contains(err.Error(), "maximum token lifetime") {
		t.Errorf("token cap error = %q, want it to mention the maximum lifetime", err)
	}

	// omniglass bootstrap <user> --ttl <over cap> errors the same way.
	err = runBootstrap(ctx, "root", "", "", "", overCap)
	if err == nil {
		t.Fatalf("runBootstrap with ttl above the cap should error, got nil")
	}
	if !strings.Contains(err.Error(), "maximum token lifetime") {
		t.Errorf("bootstrap cap error = %q, want it to mention the maximum lifetime", err)
	}
}
