package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The server side of the on-demand node run (#1163, ADR-1163): the server
// asks a node to run work now over the node's run subject and waits for its
// one answer, the simulator operation's shape. A node that is not listening,
// or does not answer within the run's budget, is an answer of its own.

// runTimeout is the longest a run may take, and so how long a node's one
// reply to a request stays permitted.
const runTimeout = 30 * time.Second

// The answers a run gets when the node does not run it (the collection
// package's, so the API names them without importing the bus).
var (
	ErrNodeAbsent        = collection.ErrNodeAbsent
	ErrNodeTimeout       = collection.ErrNodeTimeout
	ErrTaskNotOnWorklist = collection.ErrTaskNotOnWorklist
)

// RunPreview asks node to run tasks once and answer with what they read,
// storing nothing. timeout bounds the whole run, capped at the run budget.
func (s *Server) RunPreview(ctx context.Context, node string, tasks []storage.WorklistTask, timeout time.Duration) (collection.RunReply, error) {
	specs := make([]collection.TaskSpec, 0, len(tasks))
	for _, t := range tasks {
		specs = append(specs, taskSpecOf(t))
	}
	return s.runOnNode(ctx, node, collection.RunPreview, specs, timeout)
}

// RunTaskNow asks node to run one of its own worklist tasks now, landing what
// it reads as a scheduled run does. The task travels as the node's worklist
// delivers it (its secrets, its fingerprint, its gate), read fresh.
func (s *Server) RunTaskNow(ctx context.Context, node, taskID string, timeout time.Duration) (collection.RunReply, error) {
	wl, err := s.buildWorklistReply(node)
	if err != nil {
		return collection.RunReply{}, fmt.Errorf("bus: worklist for run: %w", err)
	}
	for _, t := range wl.Tasks {
		if t.ID == taskID {
			return s.runOnNode(ctx, node, collection.RunNow, []collection.TaskSpec{t}, timeout)
		}
	}
	return collection.RunReply{}, ErrTaskNotOnWorklist
}

func (s *Server) runOnNode(ctx context.Context, node, kind string, tasks []collection.TaskSpec, timeout time.Duration) (collection.RunReply, error) {
	var reply collection.RunReply
	if timeout <= 0 || timeout > runTimeout {
		timeout = runTimeout
	}
	b, err := json.Marshal(collection.RunRequest{Kind: kind, Tasks: tasks, TimeoutMs: timeout.Milliseconds()})
	if err != nil {
		return reply, err
	}
	// The node is told the budget and the wait outlasts it a little, so a run
	// that uses its whole budget still gets its answer back.
	ctx, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
	defer cancel()
	msg, err := s.nc.RequestWithContext(ctx, collection.RunSubject(node), b)
	switch {
	case errors.Is(err, nats.ErrNoResponders):
		return reply, ErrNodeAbsent
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, nats.ErrTimeout):
		return reply, ErrNodeTimeout
	case err != nil:
		return reply, fmt.Errorf("bus: run on node: %w", err)
	}
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		return reply, fmt.Errorf("bus: run reply: %w", err)
	}
	return reply, nil
}

// taskSpecOf maps a stored worklist task onto the wire, the one mapping the
// worklist reply and a run share.
func taskSpecOf(t storage.WorklistTask) collection.TaskSpec {
	return collection.TaskSpec{
		ID:             t.ID,
		Mode:           t.Mode,
		EndpointID:     t.EndpointID,
		EndpointName:   t.EndpointName,
		Transport:      t.Transport,
		EndpointParams: t.EndpointParams,
		Spec:           t.Spec,
		Secrets:        t.Secrets,
		Session:        t.Session,
		SessionRetry:   t.SessionRetry,
		HostKey:        t.HostKey,
		CertificatePin: t.CertificatePin,
		Fingerprint:    t.Fingerprint,
		Rejection:      t.Rejection,
		Ladder:         t.Ladder,
	}
}
