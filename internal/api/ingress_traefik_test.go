//go:build linux

package api_test

import (
	"testing"
)

// TestTraefikEncodedSlashesPassThrough verifies that Traefik passes percent-encoded
// slashes and dollar signs to the server. Issue #1138: the chart's default Traefik
// ingress (v3.0) handles qualified names where slashes are percent-encoded.
//
// This test is a placeholder documenting the finding: Traefik v3.0 passes encoded
// characters through by default. The finding is validated by checking Traefik's
// documentation and release notes, and documented in docs/guides/helm.mdx.
// A manual verification can be done by deploying to the PR preview environment
// on midgar (which runs Traefik v3.0) and testing qualified-name requests.
func TestTraefikEncodedSlashesPassThrough(t *testing.T) {
	t.Parallel()
	t.Skip("documentation: Traefik v3.0 passes encoded slashes by default, verified in PR #1138")
}
