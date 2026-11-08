//go:build linux

// The simulator serves on 127.x addresses beyond 127.0.0.1, which Linux
// treats as local without configuration.

package node_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// failingWorklist is the gateway with a worklist read that fails while down
// is set, or for its first failFirst reads: a database that drops out from
// under the server.
type failingWorklist struct {
	*storage.PG
	down      *atomic.Bool
	failFirst *atomic.Int64
}

func (s failingWorklist) NodeWorklist(ctx context.Context, name string) (storage.Worklist, error) {
	if s.down.Load() || s.failFirst.Add(-1) >= 0 {
		return storage.Worklist{}, errors.New("storage: node worklist: connection reset by peer")
	}
	return s.PG.NodeWorklist(ctx, name)
}

// enrolledNode creates node name on gw with an enrollment token, and returns
// the token.
func enrolledNode(t *testing.T, ctx context.Context, gw *storage.PG, name string) string {
	t.Helper()
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: name}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	token := "enroll-token-" + name
	sum := sha256.Sum256([]byte(token))
	if _, err := gw.SetEnrollmentToken(ctx, "", name, hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return token
}

// pollUntil polls cond for up to d, failing t with what if it never holds.
func pollUntil(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("after %v: %s", d, what)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// nodeLogged reports whether one of node's own log lines on gw has message
// and, when not empty, an attribute containing detail.
func nodeLogged(t *testing.T, ctx context.Context, gw *storage.PG, nodeName, message, detail string) bool {
	t.Helper()
	logs, err := gw.ListNodeLogs(ctx, nodeName, time.Hour, 500)
	if err != nil {
		t.Fatalf("node logs: %v", err)
	}
	for _, l := range logs {
		if l.Message == message && strings.Contains(string(l.Attributes), detail) {
			return true
		}
	}
	return false
}

// A node whose first worklist pulls fail does not exit (#1577): it retries on
// backoff, takes the worklist once the server can read it, and says what it
// went through in its own logs.
func TestANodeWhoseFirstPullsFailRetriesAndRuns(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	token := enrolledNode(t, ctx, gw, "edge-r")

	var down atomic.Bool
	var failFirst atomic.Int64
	failFirst.Store(2)
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, failingWorklist{gw, &down, &failFirst})
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(srv.ClientURL())))
	defer apiSrv.Close()

	runCtx, stop := context.WithCancel(ctx)
	ran := make(chan error, 1)
	go func() {
		_, err := node.Run(runCtx, node.Config{ServerURL: apiSrv.URL, Name: "edge-r", Token: token,
			HeartbeatEvery: 200 * time.Millisecond, WorklistEvery: time.Hour})
		ran <- err
	}()
	defer func() {
		stop()
		if err := <-ran; err != nil {
			t.Errorf("node run: %v", err)
		}
	}()

	// Two refused pulls, then one served: the backoff waits are under 1 s
	// and under 2 s.
	pollUntil(t, 10*time.Second, fmt.Sprintf("the node never got a worklist: %+v", srv.WorklistCounts()), func() bool {
		return srv.WorklistCounts().Served >= 1
	})
	if got := srv.WorklistCounts().ReadFailed; got != 2 {
		t.Errorf("%d failed reads, want 2", got)
	}
	select {
	case err := <-ran:
		t.Fatalf("the node exited: %v", err)
		ran <- err
	default:
	}
	// The node's own logs carry each retry and the server's reason.
	pollUntil(t, 5*time.Second, "no first-pull retry in the node's logs", func() bool {
		return nodeLogged(t, ctx, gw, "edge-r", "first worklist pull failed, retrying", "worklist read failed")
	})
}

// A transient storage error during a pull reaches the node as an error, not
// as an empty worklist (#1577): the node keeps its last one, its held session
// stays up on the device and in the session-state series, and the pull
// recovers once storage does.
func TestATransientStorageErrorLeavesTheNodesSessionsUntouched(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	panelIP := "127.89.16.7"
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}
	engine := sim.NewEngine(store, profiles.Templates())
	engine.SetPortMap(sim.EphemeralPorts)
	t.Cleanup(engine.StopAll)
	if err := engine.Sync([]sim.DeviceSnapshot{{DeviceID: "panel", Hostname: "panel", LoopbackIP: panelIP,
		ProfileName: nvpProfile(t, store), Version: 1}}); err != nil {
		t.Fatalf("serve the panel: %v", err)
	}
	panel := engine.Devices()[0]

	token := enrolledNode(t, ctx, gw, "edge-t")
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-t"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	comp, drv, nodeName := "panel-t", "newtron-nvp", "edge-t"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &nodeName,
		Inputs:     map[string]string{"host": panelIP, "port": fmt.Sprint(panelPort(t, panel, "tcp"))},
		SecretRead: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatalf("attach newtron-nvp: %v", err)
	}

	var down atomic.Bool
	var failFirst atomic.Int64
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, failingWorklist{gw, &down, &failFirst})
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(srv.ClientURL())))
	defer apiSrv.Close()

	runCtx, stop := context.WithCancel(ctx)
	ran := make(chan error, 1)
	go func() {
		_, err := node.Run(runCtx, node.Config{ServerURL: apiSrv.URL, Name: nodeName, Token: token,
			HeartbeatEvery: 200 * time.Millisecond, WorklistEvery: 200 * time.Millisecond})
		ran <- err
	}()
	defer func() {
		stop()
		if err := <-ran; err != nil {
			t.Errorf("node run: %v", err)
		}
	}()

	latest := func() string {
		v, err := gw.LatestProperty(ctx, comp, "session-state", ep.Name)
		if err != nil || v == nil {
			return ""
		}
		return v.Value
	}
	pollUntil(t, 15*time.Second, "the session never came up", func() bool { return latest() == "up" })

	// Storage drops out for three failed pulls (the backoff waits are under
	// 1 s, 2 s and 4 s).
	down.Store(true)
	failedBefore := srv.WorklistCounts().ReadFailed
	pollUntil(t, 15*time.Second, "the server never failed three pulls", func() bool {
		return srv.WorklistCounts().ReadFailed >= failedBefore+3
	})
	if n := panel.TCPSession().SessionCount(); n != 1 {
		t.Errorf("the panel holds %d sessions while storage is down, want 1", n)
	}
	if got := latest(); got != "up" {
		t.Errorf("session-state %q while storage is down, want up", got)
	}

	// Storage returns: the pull is served again and the session never moved.
	servedBefore := srv.WorklistCounts().Served
	down.Store(false)
	pollUntil(t, 15*time.Second, "the pull never recovered", func() bool {
		return srv.WorklistCounts().Served > servedBefore
	})
	if n := panel.TCPSession().SessionCount(); n != 1 {
		t.Errorf("the panel holds %d sessions after storage returned, want 1", n)
	}
	series, err := gw.ListPropertySeries(ctx, "component", comp, "session-state", time.Hour, 50)
	if err != nil {
		t.Fatalf("session-state series: %v", err)
	}
	var story []string
	for i := len(series) - 1; i >= 0; i-- {
		if series[i].Instance == ep.Name {
			story = append(story, series[i].Value)
		}
	}
	if got := strings.Join(story, ","); got != "establishing,up" {
		t.Errorf("session-state series %s, want establishing,up", got)
	}
	// The node read the refusal as an error naming the server's reason, not
	// a timeout.
	pollUntil(t, 5*time.Second, "no failed pull naming the read failure in the node's logs", func() bool {
		return nodeLogged(t, ctx, gw, nodeName, "worklist pull failed, the node keeps its last worklist", "worklist read failed")
	})
}
