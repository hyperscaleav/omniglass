package bus

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Config configures the embedded NATS server. Port -1 binds an ephemeral port
// (tests always use -1; a fixed host port is never bound). StoreDir holds the
// JetStream state; empty means a fresh temp dir owned and cleaned by the Server.
type Config struct {
	Host     string
	Port     int
	StoreDir string
}

// Server is the embedded NATS server plus the server-side control-plane handlers.
type Server struct {
	ns            *server.Server
	nc            *nats.Conn
	store         Store
	subs          []*nats.Subscription
	consumeCtx    jetstream.ConsumeContext // the telemetry durable consumer's pull loop
	internalToken string
	ownStoreDir   string // non-empty when the Server created (and must clean) it
}

// New starts an in-process NATS server (JetStream on) with the per-node auth
// callback, then opens the server's own internal client and subscribes the
// worklist and heartbeat handlers. It returns ready to serve nodes.
func New(cfg Config, store Store) (*Server, error) {
	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		return nil, fmt.Errorf("bus: internal token: %w", err)
	}
	s := &Server{store: store, internalToken: hex.EncodeToString(tok)}

	storeDir := cfg.StoreDir
	if storeDir == "" {
		d, err := os.MkdirTemp("", "omniglass-nats-")
		if err != nil {
			return nil, fmt.Errorf("bus: jetstream store dir: %w", err)
		}
		storeDir = d
		s.ownStoreDir = d
	}

	opts := &server.Options{
		Host:                       cfg.Host,
		Port:                       cfg.Port,
		JetStream:                  true,
		StoreDir:                   storeDir,
		NoLog:                      true,
		NoSigs:                     true,
		CustomClientAuthentication: &nodeAuth{store: store, internalToken: s.internalToken},
	}
	ns, err := server.NewServer(opts)
	if err != nil {
		s.cleanupStoreDir()
		return nil, fmt.Errorf("bus: new server: %w", err)
	}
	s.ns = ns
	ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		s.Shutdown()
		return nil, fmt.Errorf("bus: nats-server not ready for connections")
	}
	if err := s.subscribe(); err != nil {
		s.Shutdown()
		return nil, err
	}
	return s, nil
}

// subscribe opens the internal client and wires the two server-side handlers.
func (s *Server) subscribe() error {
	nc, err := nats.Connect(s.ns.ClientURL(), nats.Token(s.internalToken), nats.Name("omniglass-server"))
	if err != nil {
		return fmt.Errorf("bus: internal client connect: %w", err)
	}
	s.nc = nc

	wl, err := nc.Subscribe(collection.WorklistWildcard, func(msg *nats.Msg) {
		node := collection.NodeFromSubject(msg.Subject)
		// Confused-deputy guard: the responder answers with the FULL-PERMISSION
		// internal client, and msg.Reply is attacker-controlled, so honor it only
		// when it lands in the requesting node's own inbox. Otherwise a node could
		// aim the reply at another node's subject (heartbeat forge) or, once a
		// stream exists, at $JS.API.*/$SYS.*. The node client dials this inbox via
		// nats.CustomInboxPrefix(collection.InboxPrefix(node)), so a real reply is
		// InboxPrefix(node)+"."+<token>.
		if msg.Reply == "" || !strings.HasPrefix(msg.Reply, collection.InboxPrefix(node)+".") {
			return
		}
		reply, err := s.buildWorklistReply(node)
		if err != nil {
			return // read failed; drop, the node re-pulls next tick
		}
		b, err := json.Marshal(reply)
		if err != nil {
			return
		}
		_ = msg.Respond(b)
	})
	if err != nil {
		return fmt.Errorf("bus: subscribe worklist: %w", err)
	}

	hb, err := nc.Subscribe(collection.HeartbeatWildcard, func(msg *nats.Msg) {
		node := collection.NodeFromSubject(msg.Subject)
		// The subject grant guarantees this node published only its own subject,
		// so the extracted name is trusted.
		_ = s.store.RecordHeartbeat(context.Background(), node)
	})
	if err != nil {
		return fmt.Errorf("bus: subscribe heartbeat: %w", err)
	}
	s.subs = append(s.subs, wl, hb)

	// The telemetry ingest path: a JetStream stream + durable consumer over the
	// same internal client. It carries the node -> server datapoint flow.
	if err := s.startTelemetryConsumer(); err != nil {
		return fmt.Errorf("bus: start telemetry consumer: %w", err)
	}
	return nil
}

// buildWorklistReply maps a node's stored worklist to the JSON wire reply.
func (s *Server) buildWorklistReply(node string) (collection.WorklistReply, error) {
	wl, err := s.store.NodeWorklist(context.Background(), node)
	if err != nil {
		return collection.WorklistReply{}, err
	}
	reply := collection.WorklistReply{ConfigGeneration: wl.ConfigGeneration}
	for _, t := range wl.Tasks {
		reply.Tasks = append(reply.Tasks, collection.TaskSpec{
			ID:              t.ID,
			Mode:            t.Mode,
			InterfaceName:   t.InterfaceName,
			InterfaceType:   t.InterfaceType,
			InterfaceParams: t.InterfaceParams,
			Spec:            t.Spec,
		})
	}
	return reply, nil
}

// ClientURL is the URL a node dials (nats://host:port).
func (s *Server) ClientURL() string { return s.ns.ClientURL() }

// Shutdown drains the internal client and stops the server, then removes an
// owned JetStream store dir. Idempotent enough to defer.
func (s *Server) Shutdown() {
	for _, sub := range s.subs {
		_ = sub.Unsubscribe()
	}
	// Stop the telemetry consumer's pull loop before closing the client it runs on.
	if s.consumeCtx != nil {
		s.consumeCtx.Stop()
		s.consumeCtx = nil
	}
	if s.nc != nil {
		s.nc.Close()
	}
	if s.ns != nil {
		s.ns.Shutdown()
		s.ns.WaitForShutdown()
	}
	s.cleanupStoreDir()
}

func (s *Server) cleanupStoreDir() {
	if s.ownStoreDir != "" {
		_ = os.RemoveAll(s.ownStoreDir)
		s.ownStoreDir = ""
	}
}
