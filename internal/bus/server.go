package bus

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
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

	// The command queue pull (#815): request-reply like the worklist, same
	// reply-inbox confinement, the reply the node's rendered pending commands
	// (the pull itself marks them dispatched).
	cq, err := nc.Subscribe(collection.CommandWildcard, func(msg *nats.Msg) {
		node := collection.NodeFromSubject(msg.Subject)
		if msg.Reply == "" || !strings.HasPrefix(msg.Reply, collection.InboxPrefix(node)+".") {
			return
		}
		pending, err := s.store.PendingNodeCommands(context.Background(), node)
		if err != nil {
			return // read failed; drop, the node re-pulls next tick
		}
		reply := collection.CommandPullReply{}
		for _, d := range pending {
			reply.Commands = append(reply.Commands, collection.CommandDelivery{
				ID: d.ID, CommandType: d.CommandType, Transport: d.Transport, Target: d.Target, Line: d.Line,
			})
		}
		b, err := json.Marshal(reply)
		if err != nil {
			return
		}
		_ = msg.Respond(b)
	})
	if err != nil {
		return fmt.Errorf("bus: subscribe command queue: %w", err)
	}

	// The execution report sink: the subject grant trusts the node name, and
	// the store re-checks the placement before stamping.
	cs, err := nc.Subscribe(collection.CommandStatusWildcard, func(msg *nats.Msg) {
		node := collection.NodeFromSubject(msg.Subject)
		var st collection.CommandStatus
		if err := json.Unmarshal(msg.Data, &st); err != nil {
			return
		}
		// A report a node had no standing to make (a command dispatched to a
		// different node, or already stamped) is logged, never silently
		// dropped: it is either a race or a misbehaving node, and both want a
		// trail.
		if err := s.store.RecordCommandExecution(context.Background(), node, st.ID, st.Error); err != nil {
			slog.Warn("command execution report rejected", "facility", "command", "node", node, "command", st.ID, "error", err.Error())
		}
	})
	if err != nil {
		return fmt.Errorf("bus: subscribe command status: %w", err)
	}
	s.subs = append(s.subs, wl, hb, cq, cs)

	// The telemetry ingest path: a JetStream stream + durable consumer over the
	// same internal client. It carries the node -> server sample flow.
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
			ID:             t.ID,
			Mode:           t.Mode,
			EndpointName:   t.EndpointName,
			Transport:      t.Transport,
			EndpointParams: t.EndpointParams,
			Spec:           t.Spec,
			Secrets:        t.Secrets,
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

// publishFlushTimeout bounds the post-publish flush (see PublishTelemetry).
const publishFlushTimeout = 5 * time.Second

// PublishTelemetry puts an already-authorized batch on the API telemetry lane. It
// satisfies the API's TelemetryPublisher seam, so the HTTP handler never holds a
// NATS connection of its own. Publishing uses the server's internal credential, the
// only one whose grant reaches this subject, which is what makes the consumer's
// "believe the owner on this subject" rule safe.
func (s *Server) PublishTelemetry(ctx context.Context, b *ogv1.TelemetryBatch) error {
	raw, err := proto.Marshal(b)
	if err != nil {
		return fmt.Errorf("bus: marshal telemetry batch: %w", err)
	}
	if err := s.nc.Publish(collection.APITelemetrySubject, raw); err != nil {
		return fmt.Errorf("bus: publish telemetry: %w", err)
	}
	// Flush so a 202 means the broker actually has the batch, not merely that we
	// queued it locally (an unflushed publish is lost silently if the connection
	// drops). The flush gets its own bound rather than inheriting the caller's
	// context: FlushWithContext REQUIRES a deadline, and an HTTP request context has
	// none, so passing it straight through fails every publish. WithTimeout also
	// keeps a shorter caller deadline if there is one.
	ctx, cancel := context.WithTimeout(ctx, publishFlushTimeout)
	defer cancel()
	if err := s.nc.FlushWithContext(ctx); err != nil {
		return fmt.Errorf("bus: flush telemetry publish: %w", err)
	}
	return nil
}
