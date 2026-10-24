//go:build !linux

package nettest

import "testing"

// RefusedAddr skips the test off Linux: the hold relies on Linux's bind
// semantics, and under BSD's a wildcard listener with SO_REUSEADDR can bind
// beside the held address and answer the dial.
func RefusedAddr(t testing.TB) string {
	t.Helper()
	t.Skip("nettest.RefusedAddr holds a port by Linux bind semantics")
	return ""
}
