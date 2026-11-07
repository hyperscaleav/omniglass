package lab

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// scaleServer is a scale lab's API as readiness reads it: each site's node
// simulator, connected unless named in silent, serving its components, each
// up unless named in down.
func scaleServer(t *testing.T, sites map[string][]string, silent, down map[string]bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/healthz", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /api/v1/nodes/{name}/simulation", func(w http.ResponseWriter, r *http.Request) {
		node := r.PathValue("name")
		comps, ok := sites[node]
		if !ok {
			http.NotFound(w, r)
			return
		}
		var devs []string
		for _, c := range comps {
			devs = append(devs, fmt.Sprintf(`{"component_id":"id-%s","component":%q,"status":"running"}`, c, c))
		}
		fmt.Fprintf(w, `{"connected":%v,"devices":[%s]}`, !silent[node], strings.Join(devs, ","))
	})
	mux.HandleFunc("GET /api/v1/components/{id}/reachability", func(w http.ResponseWriter, r *http.Request) {
		verdict := "up"
		if down[strings.TrimPrefix(r.PathValue("id"), "id-")] {
			verdict = "down"
		}
		fmt.Fprintf(w, `{"endpoints":[{"label":"","verdict":{"value":%q}}]}`, verdict)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

var testSites = map[string][]string{
	"edge-hq":        {"hq-display-1", "hq-codec-1", "hq-dsp-1"},
	"edge-branch-01": {"branch-01-display-1"},
}

func scaleConfig() Config {
	cfg := testConfig()
	cfg.PlantedFaults = nil
	cfg.Project, cfg.Ephemeral, cfg.Scenario, cfg.Systems = "omniglass-scale", true, "corporate", 20
	cfg.SiteNodes = []SiteNode{
		{Name: "edge-hq", Token: "t1", TokenHash: "h1", Components: 3},
		{Name: "edge-branch-01", Token: "t2", TokenHash: "h2", Components: 1},
	}
	cfg.ScaleRun = &ScaleRun{Shape: linearShape, MinSystems: 10, MaxSystems: 3000, Cost: testCost, Timeout: 2 * time.Second}
	return cfg
}

// A scale lab starts one node per site, after the seed and the server, and
// is up once every site's components are.
func TestScaleRunBringsUpOneNodePerSite(t *testing.T) {
	srv := scaleServer(t, testSites, nil, nil)
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	var out bytes.Buffer
	lab, err := testRunner(eng, &out).Run(context.Background(), scaleConfig())
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	want := "network omniglass-scale_default,container db,container seed,container server,container edge-hq,container edge-branch-01"
	if got := strings.Join(eng.created, ","); got != want {
		t.Fatalf("created %s, want %s", got, want)
	}
	if lab.ID("edge-hq") == "" || lab.ID("edge-branch-01") == "" || lab.ID("node") != "" {
		t.Fatalf("container ids %v", lab.ids)
	}
	if len(eng.containers) != 4 {
		t.Fatalf("%d containers running, want db, server and a node per site", len(eng.containers))
	}
	for _, line := range []string{"2 of 2 sites up; 4 of 4 simulated components up", "2, one per site", "capacity"} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("status lacks %q:\n%s", line, out.String())
		}
	}
}

// A size the host cannot fit is refused before anything is created, naming
// the largest size that fits; a host too small for any is told so.
func TestScaleRunRefusesASizeTheHostCannotFit(t *testing.T) {
	srv := scaleServer(t, testSites, nil, nil)
	for name, tc := range map[string]struct {
		host HostResources
		want string
	}{
		// 600 MiB, of which 480 usable: 100 + 10*(1+n/10) + 3n MiB is 479
		// at 93 systems (10 sites) and 483 at 94.
		"some size fits": {HostResources{MemBytes: 600 << 20, CPUs: 100}, "--systems 93, the largest that fits"},
		"none fits":      {HostResources{MemBytes: 64 << 20, CPUs: 100}, "Not even the smallest size fits"},
	} {
		t.Run(name, func(t *testing.T) {
			eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
			cfg := scaleConfig()
			cfg.Systems, cfg.SiteNodes[0].Components = 1000, 3000
			cfg.ScaleRun.Host = &tc.host
			_, err := testRunner(eng, &bytes.Buffer{}).Run(context.Background(), cfg)
			var pe *PreflightError
			if !errors.As(err, &pe) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want a refusal naming %q", err, tc.want)
			}
			if len(eng.created) != 0 {
				t.Fatalf("created %v before refusing", eng.created)
			}
		})
	}
	// The Docker host's own report is what is checked without a forced limit.
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	eng.host = HostResources{MemBytes: 64 << 20, CPUs: 1}
	if _, err := testRunner(eng, &bytes.Buffer{}).Run(context.Background(), scaleConfig()); err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("got %v, want the host's own 64 MiB refused", err)
	}
}

// A bring-up that stalls past its timeout names each site not up and what
// of it is not, then leaves no container or network behind.
func TestScaleRunTimesOutNamingWhatIsNotUp(t *testing.T) {
	srv := scaleServer(t, testSites, map[string]bool{"edge-branch-01": true}, map[string]bool{"hq-dsp-1": true})
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	cfg := scaleConfig()
	cfg.ScaleRun.Timeout = 300 * time.Millisecond
	var out bytes.Buffer
	_, err := testRunner(eng, &out).Run(context.Background(), cfg)
	var notUp *SitesNotUpError
	if !errors.As(err, &notUp) {
		t.Fatalf("got %v, want the sites not up named", err)
	}
	for _, want := range []string{"within 300ms", "edge-hq: 1 of 3 components not up: hq-dsp-1", "edge-branch-01: its simulator has not connected"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q lacks %q", err, want)
		}
	}
	if len(eng.containers) != 0 || len(eng.networks) != 0 || len(eng.volumes) != 0 {
		t.Fatalf("after the timeout: containers %v networks %v volumes %v, want none", eng.containers, eng.networks, eng.volumes)
	}
}

// A timeout before the sites are waited on (a seed that never finishes)
// names the stage it ran out in and every site as not up.
func TestScaleRunTimesOutInTheSeed(t *testing.T) {
	srv := scaleServer(t, testSites, nil, nil)
	eng := newFakeEngine(strings.TrimPrefix(srv.URL, "http://"))
	eng.seedHangs = true
	cfg := scaleConfig()
	cfg.ScaleRun.Timeout = 200 * time.Millisecond
	_, err := testRunner(eng, &bytes.Buffer{}).Run(context.Background(), cfg)
	for _, want := range []string{"seeding the fleet", "edge-hq: its simulator has not connected", "edge-branch-01"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("got %v, want it to name %q", err, want)
		}
	}
	if len(eng.containers) != 0 || len(eng.networks) != 0 {
		t.Fatalf("after the timeout: containers %v networks %v, want none", eng.containers, eng.networks)
	}
}

func TestScaleRunNeedsItsSettings(t *testing.T) {
	cfg := scaleConfig()
	cfg.ScaleRun = nil
	if _, err := testRunner(newFakeEngine("x"), &bytes.Buffer{}).Run(context.Background(), cfg); err == nil {
		t.Fatal("a scale lab ran without its scale settings")
	}
}

func TestFormatSitesNotUpCountsPastTheFirstTwenty(t *testing.T) {
	var names []string
	for i := range 25 {
		names = append(names, fmt.Sprintf("c%d", i))
	}
	got := FormatSitesNotUp([]SiteState{
		{Node: "edge-a", Connected: true, Want: 3, Listed: 3, Up: 3},
		{Node: "edge-b", Connected: true, Want: 30, Listed: 28, Up: 3, NotUp: names},
	})
	if strings.Contains(got, "edge-a") {
		t.Errorf("a site up was named: %s", got)
	}
	for _, want := range []string{"edge-b: its simulator lists 28 of its 30 components; 27 of 30 components not up: c0,", "c19, and 5 more"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
}
