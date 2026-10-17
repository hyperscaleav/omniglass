package simnode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// Config is the simulator run mode's configuration. It authenticates as the
// node it runs beside, with that node's name and enrollment token.
type Config struct {
	ServerURL string
	Name      string
	Token     string
	// PortOffset shifts every profile port, so a simulator without the
	// privilege to bind ports below 1024 can still serve (the node's
	// interfaces must target the shifted ports).
	PortOffset int
	// Poll is how often the simulator re-pulls its device list; FlushEvery
	// how often it publishes the hits it saw. Zero takes the defaults.
	Poll       time.Duration
	FlushEvery time.Duration
}

const (
	defaultPoll       = 10 * time.Second
	defaultFlushEvery = time.Second
)

// Run serves the node's simulated devices until ctx is cancelled: it pulls
// the list, serves what it can, follows changes on every pull without a
// restart, and publishes hits. A device it cannot serve is logged, not
// fatal.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Poll <= 0 {
		cfg.Poll = defaultPoll
	}
	if cfg.FlushEvery <= 0 {
		cfg.FlushEvery = defaultFlushEvery
	}
	log := slog.Default().With("sim", cfg.Name)

	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		return fmt.Errorf("sim: load catalog: %w", err)
	}
	creds, err := node.Claim(ctx, cfg.ServerURL, cfg.Name, cfg.Token)
	if err != nil {
		return err
	}
	nc, err := nats.Connect(creds.NatsURL,
		nats.UserInfo(creds.Username, creds.Password),
		nats.CustomInboxPrefix(collection.InboxPrefix(cfg.Name)),
		nats.Name("omniglass-sim-"+cfg.Name),
	)
	if err != nil {
		return fmt.Errorf("sim: connect bus at %s: %w", creds.NatsURL, err)
	}
	defer nc.Close()

	caps := detectCapabilities(cfg.PortOffset)
	log.Info("simulator connected", "url", creds.NatsURL, "low_ports", caps.LowPorts,
		"net_admin", caps.NetAdmin, "port_offset", caps.PortOffset)

	engine := sim.NewEngine(store, profiles.Templates())
	if cfg.PortOffset != 0 {
		engine.SetPortMap(func(p int) int { return p + cfg.PortOffset })
	}
	defer engine.StopAll()
	hits := &hitBuffer{}
	engine.SetHitReporter(hits.add)

	hasProfile := func(p string) bool { _, ok := store.Devices[p]; return ok }
	pull := func() {
		devices, err := pullList(nc, cfg.Name, caps)
		if err != nil {
			log.Warn("device list pull failed", "error", err)
			return
		}
		snaps, skips := plan(devices, hasProfile)
		for _, s := range skips {
			log.Debug("not simulating", "component", s.Name, "reason", s.Reason)
		}
		if err := engine.Sync(snaps); err != nil {
			log.Warn("some devices did not start", "error", err)
		}
	}
	pull()
	pollT := time.NewTicker(cfg.Poll)
	defer pollT.Stop()
	flushT := time.NewTicker(cfg.FlushEvery)
	defer flushT.Stop()
	for {
		select {
		case <-ctx.Done():
			flush(nc, cfg.Name, engine, hits)
			return nil
		case <-pollT.C:
			pull()
		case <-flushT.C:
			flush(nc, cfg.Name, engine, hits)
		}
	}
}

func pullList(nc *nats.Conn, name string, caps collection.SimCapabilities) ([]collection.SimDevice, error) {
	req, err := json.Marshal(collection.SimlistRequest{Capabilities: caps})
	if err != nil {
		return nil, err
	}
	msg, err := nc.Request(collection.SimlistSubject(name), req, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var reply collection.SimlistReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		return nil, err
	}
	return reply.Devices, nil
}

// flush publishes the hits seen since the last flush with the devices being
// served now. It publishes even with no hits, so the server's view of what
// is serving stays current.
func flush(nc *nats.Conn, name string, engine *sim.Engine, hits *hitBuffer) {
	batch := collection.SimHitBatch{Hits: hits.drain()}
	for _, d := range engine.Devices() {
		batch.Serving = append(batch.Serving, d.DeviceID)
	}
	b, err := json.Marshal(batch)
	if err != nil {
		return
	}
	_ = nc.Publish(collection.SimHitSubject(name), b)
}

// hitBuffer collects engine hits between flushes, mapped to the wire shape
// (a device's id is its component id).
type hitBuffer struct {
	mu   sync.Mutex
	hits []collection.SimHit
}

func (b *hitBuffer) add(h sim.Hit) {
	b.mu.Lock()
	b.hits = append(b.hits, collection.SimHit{
		ComponentID: h.DeviceID, Protocol: h.Protocol, Method: h.Method,
		Path: h.Path, Port: h.Port, Status: h.Status, At: h.At,
	})
	b.mu.Unlock()
}

func (b *hitBuffer) drain() []collection.SimHit {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.hits
	b.hits = nil
	return out
}

// detectCapabilities reads what this process may do from the kernel. Where
// the files do not exist (not Linux), it states nothing beyond the offset.
func detectCapabilities(portOffset int) collection.SimCapabilities {
	var capEff uint64
	if b, err := os.ReadFile("/proc/self/status"); err == nil {
		capEff, _ = parseCapEff(string(b))
	}
	unpriv := 1024
	if b, err := os.ReadFile("/proc/sys/net/ipv4/ip_unprivileged_port_start"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
			unpriv = n
		}
	}
	return capabilitiesFrom(capEff, unpriv, portOffset)
}
