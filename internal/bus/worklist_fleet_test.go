package bus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/nats-io/nats.go"
)

// fleetWireCap is the most an HQ worklist may weigh on the wire at 1,000
// systems (#1577): half of max payload, the headroom the next size step needs.
const fleetWireCap = 512 * 1024

// Every node of the 1,000-system corporate lab pulls its worklist at once,
// over the real bus and the bounded responder: each pull is answered, none
// queues past its budget, and HQ's (over 800 components) is gzipped under half
// of max payload (#1577).
func TestAFleetOfNodesPullingAtOnceIsServedByTheBoundedResponder(t *testing.T) {
	if testing.Short() {
		t.Skip("seeds the 1,000-system corporate lab")
	}
	gw, plan, hq, nodeOf := corporateFleet(t, 1000)
	ctx := context.Background()
	all := scope.Set{All: true}
	tokens := map[string]string{}
	for _, s := range plan.Sites {
		name, token := nodeOf(s.Name), "secret-"+s.Name
		sum := sha256.Sum256([]byte(token))
		if _, err := gw.SetEnrollmentToken(ctx, "", name, hex.EncodeToString(sum[:]), all); err != nil {
			t.Fatal(err)
		}
		tokens[name] = token
	}

	srv, err := New(Config{Host: "127.0.0.1", Port: -1, DBPoolSize: gw.PoolSize()}, gw)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()
	conns := map[string]*nats.Conn{}
	for name, token := range tokens {
		nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo(name, token), nats.CustomInboxPrefix(collection.InboxPrefix(name)))
		if err != nil {
			t.Fatal(err)
		}
		defer nc.Close()
		conns[name] = nc
	}

	type pulled struct {
		wire, tasks int
		err         error
	}
	var (
		mu    sync.Mutex
		got   = map[string]pulled{}
		start = make(chan struct{})
		wg    sync.WaitGroup
	)
	for name, nc := range conns {
		wg.Go(func() {
			req := nats.NewMsg(collection.WorklistSubject(name))
			req.Header.Set(collection.WorklistTimeoutHeader, "5s")
			<-start
			msg, err := nc.RequestMsg(req, 5*time.Second)
			p := pulled{err: err}
			if err == nil {
				p.wire = len(msg.Data)
				var wl collection.WorklistReply
				wl, p.err = collection.DecodeWorklistReply(msg.Header, msg.Data)
				p.tasks = len(wl.Tasks)
			}
			mu.Lock()
			got[name] = p
			mu.Unlock()
		})
	}
	began := time.Now()
	close(start)
	wg.Wait()
	t.Logf("%d nodes pulled at once with %d workers in %s", len(conns), srv.worklistWorkers, time.Since(began))

	for _, s := range plan.Sites {
		name := nodeOf(s.Name)
		p := got[name]
		if p.err != nil {
			t.Errorf("%s: %v", name, p.err)
			continue
		}
		if p.tasks == 0 {
			t.Errorf("%s pulled an empty worklist", name)
		}
		if s.Archetype != hq {
			continue
		}
		t.Logf("HQ %s: %d components, %d tasks, %d bytes on the wire", name, s.Totals.Components, p.tasks, p.wire)
		if s.Totals.Components < 800 {
			t.Errorf("HQ has %d components, the test needs at least 800", s.Totals.Components)
		}
		if p.wire > fleetWireCap {
			t.Errorf("HQ's worklist is %d bytes on the wire, over %d", p.wire, fleetWireCap)
		}
	}
	c := srv.WorklistCounts()
	if c.Served != int64(len(conns)) || c.Expired != 0 || c.Abandoned != 0 || c.Oversize != 0 || c.ReadFailed != 0 || c.RespondFailed != 0 {
		t.Errorf("counts %+v, want %d served and nothing else", c, len(conns))
	}
}
