package cli

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// A catalog positional takes ns/key or a bare name (#1434, ADR-0318): ns/key
// goes straight to its two-segment route; a bare name follows the search
// path, local first and og when local holds no such key, by one lookup before
// the qualified call; a child key on a qualified parent resolves the same way.
func TestACatalogPositionalTakesNamespaceKeyOrABareName(t *testing.T) {
	local := map[string]bool{"/api/v1/metric-types/local/mine": true}
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodGet && len(r.URL.Path) > len("/api/v1/metric-types/local/") &&
			r.URL.Path[:len("/api/v1/metric-types/local/")] == "/api/v1/metric-types/local/" && !local[r.URL.Path] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"metric-type", "get", "og/icmp-rtt-avg"}, []string{"GET /api/v1/metric-types/og/icmp-rtt-avg"}},
		{[]string{"metric-type", "get", "mine"}, []string{"GET /api/v1/metric-types/local/mine", "GET /api/v1/metric-types/local/mine"}},
		{[]string{"metric-type", "get", "icmp-rtt-avg"}, []string{"GET /api/v1/metric-types/local/icmp-rtt-avg", "GET /api/v1/metric-types/og/icmp-rtt-avg"}},
		{[]string{"product", "metric", "delete", "local/panel", "room-temperature"}, []string{
			"GET /api/v1/metric-types/local/room-temperature", "DELETE /api/v1/products/local/panel/metrics/og/room-temperature"}},
	} {
		seen = nil
		out, err := runCLI(t, append(tc.args, "--server", srv.URL)...)
		if err != nil {
			t.Fatalf("%v: %v (%s)", tc.args, err, out)
		}
		if !slices.Equal(seen, tc.want) {
			t.Errorf("%v sent %v, want %v", tc.args, seen, tc.want)
		}
	}
	if _, err := runCLI(t, "metric-type", "get", "og/..", "--server", srv.URL); err == nil {
		t.Error("og/.. was accepted; a dot segment addresses no resource")
	}
}
