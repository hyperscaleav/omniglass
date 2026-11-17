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

// The server side of the on-demand outpost run (#1163, ADR-1163): the server
// asks an outpost to run work now over the outpost's run subject and waits for its
// one answer, the simulator operation's shape. An outpost that is not listening,
// or does not answer within the run's budget, is an answer of its own.

// runTimeout is the longest a run may take, and so how long an outpost's one
// reply to a request stays permitted.
const runTimeout = 30 * time.Second

// runReplySlack is how long past a run's budget its answer is still waited
// for, and still permitted: a run that uses its whole budget answers a little
// after it.
const runReplySlack = 3 * time.Second

// The answers a run gets when the outpost does not run it (the collection
// package's, so the API names them without importing the bus).
var (
	ErrOutpostAbsent     = collection.ErrOutpostAbsent
	ErrOutpostTimeout    = collection.ErrOutpostTimeout
	ErrTaskNotOnWorklist = collection.ErrTaskNotOnWorklist
)

// RunPreview asks outpost to run tasks once and answer with what they read,
// storing nothing; mask is the secret field values the outpost scrubs from what
// it answers. timeout bounds the whole run, capped at the run budget.
func (s *Server) RunPreview(ctx context.Context, outpost string, tasks []storage.WorklistTask, mask []string, timeout time.Duration) (collection.RunReply, error) {
	specs := make([]collection.TaskSpec, 0, len(tasks))
	for _, t := range tasks {
		specs = append(specs, taskSpecOf(t))
	}
	return s.runOnOutpost(ctx, outpost, collection.RunRequest{Kind: collection.RunPreview, Tasks: specs, Mask: mask}, timeout)
}

// RunTaskNow asks outpost to run one of its own worklist tasks now, landing what
// it reads as a scheduled run does. The task travels as the outpost's worklist
// delivers it (its secrets, its fingerprint, its gate), read fresh.
func (s *Server) RunTaskNow(ctx context.Context, outpost, taskID string, timeout time.Duration) (collection.RunReply, error) {
	wl, err := s.buildWorklistReply(ctx, outpost)
	if err != nil {
		return collection.RunReply{}, fmt.Errorf("bus: worklist for run: %w", err)
	}
	for _, t := range wl.Tasks {
		if t.ID == taskID {
			return s.runOnOutpost(ctx, outpost, collection.RunRequest{Kind: collection.RunNow, Tasks: []collection.TaskSpec{t}}, timeout)
		}
	}
	return collection.RunReply{}, ErrTaskNotOnWorklist
}

func (s *Server) runOnOutpost(ctx context.Context, outpost string, req collection.RunRequest, timeout time.Duration) (collection.RunReply, error) {
	var reply collection.RunReply
	if timeout <= 0 || timeout > runTimeout {
		timeout = runTimeout
	}
	req.TimeoutMs = timeout.Milliseconds()
	b, err := json.Marshal(req)
	if err != nil {
		return reply, err
	}
	// The outpost is told the budget and the wait outlasts it a little, so a run
	// that uses its whole budget still gets its answer back.
	ctx, cancel := context.WithTimeout(ctx, timeout+runReplySlack)
	defer cancel()
	msg, err := s.nc.RequestWithContext(ctx, collection.RunSubject(outpost), b)
	switch {
	case errors.Is(err, nats.ErrNoResponders):
		return reply, ErrOutpostAbsent
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, nats.ErrTimeout):
		return reply, ErrOutpostTimeout
	case err != nil:
		return reply, fmt.Errorf("bus: run on outpost: %w", err)
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
		Mask:           t.Mask,
		Session:        t.Session,
		SessionRetry:   t.SessionRetry,
		HostKey:        t.HostKey,
		CertificatePin: t.CertificatePin,
		Fingerprint:    t.Fingerprint,
		Rejection:      t.Rejection,
		Ladder:         t.Ladder,
		ComponentID:    componentOnWire(t),
		PathUpstreams:  pathUpstreamsOf(t.PathUpstreams),
	}
}

// componentOnWire is the task's component when it is reached through a path
// upstream, else none (#1652): the outpost reads an upstream's component off the
// edge that names it, so the tasks reached through nothing, most of a large
// worklist, pay no bytes for it.
func componentOnWire(t storage.WorklistTask) string {
	if len(t.PathUpstreams) == 0 {
		return ""
	}
	return t.ComponentID
}

// pathUpstreamsOf maps a task's stored path upstreams onto the wire (#1652).
func pathUpstreamsOf(ups []storage.PathUpstream) []collection.PathUpstream {
	if len(ups) == 0 {
		return nil
	}
	out := make([]collection.PathUpstream, len(ups))
	for i, u := range ups {
		out[i] = collection.PathUpstream{ComponentID: u.ComponentID, ComponentName: u.ComponentName, EndpointIDs: u.EndpointIDs}
	}
	return out
}
