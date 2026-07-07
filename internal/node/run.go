package node

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/nats-io/nats.go"
)

// worklistTimeout bounds a single worklist request-reply.
const worklistTimeout = 5 * time.Second

// Config is the node run-mode configuration.
type Config struct {
	ServerURL      string
	Name           string
	Token          string
	HeartbeatEvery time.Duration
	// Once runs a single claim + pull + heartbeat cycle and returns, for tests
	// and one-shot invocations.
	Once bool
}

// Run claims the node's NATS credential, connects outbound-only to the bus,
// pulls its worklist, and heartbeats until the context is cancelled (or a single
// cycle if Once). It returns the last worklist it pulled.
func Run(ctx context.Context, cfg Config) (collection.WorklistReply, error) {
	creds, err := Claim(ctx, cfg.ServerURL, cfg.Name, cfg.Token)
	if err != nil {
		return collection.WorklistReply{}, err
	}
	nc, err := nats.Connect(creds.NatsURL,
		nats.UserInfo(creds.Username, creds.Password),
		nats.CustomInboxPrefix(collection.InboxPrefix(cfg.Name)),
		nats.Name("omniglass-node-"+cfg.Name),
	)
	if err != nil {
		return collection.WorklistReply{}, fmt.Errorf("node: connect bus at %s: %w", creds.NatsURL, err)
	}
	defer nc.Close()

	wl, err := pullWorklist(nc, cfg.Name)
	if err != nil {
		return collection.WorklistReply{}, err
	}
	if err := publishHeartbeat(nc, cfg.Name); err != nil {
		return wl, err
	}

	if cfg.Once {
		_ = nc.Flush()
		return wl, nil
	}

	every := cfg.HeartbeatEvery
	if every <= 0 {
		every = 30 * time.Second
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = nc.Flush()
			return wl, nil
		case <-ticker.C:
			_ = publishHeartbeat(nc, cfg.Name)
			// Re-pull each tick; a changed config generation drives a refresh in
			// later checkpoints. A pull failure is non-fatal (retry next tick).
			if next, err := pullWorklist(nc, cfg.Name); err == nil {
				wl = next
			}
		}
	}
}

// pullWorklist runs the worklist request-reply and decodes the reply.
func pullWorklist(nc *nats.Conn, name string) (collection.WorklistReply, error) {
	msg, err := nc.Request(collection.WorklistSubject(name), nil, worklistTimeout)
	if err != nil {
		return collection.WorklistReply{}, fmt.Errorf("node: pull worklist: %w", err)
	}
	var reply collection.WorklistReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		return collection.WorklistReply{}, fmt.Errorf("node: decode worklist: %w", err)
	}
	return reply, nil
}

// publishHeartbeat sends one liveness heartbeat on the node's own subject.
func publishHeartbeat(nc *nats.Conn, name string) error {
	b, err := json.Marshal(collection.Heartbeat{Node: name, At: time.Now().UTC()})
	if err != nil {
		return fmt.Errorf("node: encode heartbeat: %w", err)
	}
	if err := nc.Publish(collection.HeartbeatSubject(name), b); err != nil {
		return fmt.Errorf("node: publish heartbeat: %w", err)
	}
	return nil
}
