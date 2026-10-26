package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The node side of the on-demand node run (#1163, ADR-1163): the server asks
// the node to run work now and the node answers once. A preview runs polls of
// a spec no endpoint holds yet, on the request's own goroutine, touching no
// held session and publishing nothing. A run now runs one of the node's own
// poll tasks on the run loop, where every held session and gate lives, so it
// borrows the endpoint's session and honors its gate exactly as a scheduled
// run does, and lands what it reads.

// defaultRunTimeout bounds a run whose request names no budget.
const defaultRunTimeout = 20 * time.Second

// runJob is a run now handed to the run loop: the task, its budget, and the
// one answer the request is owed.
type runJob struct {
	task collection.TaskSpec
	// deadline is stamped when the request arrives, so a job the loop takes
	// late runs only in what remains of its caller's budget, and one whose
	// budget is gone is dropped unrun (its caller was already answered).
	deadline time.Time
	respond  func(collection.RunReply)
}

// subscribeRuns takes the server's run requests for this node. A preview runs
// at once on its own goroutine; a run now is queued to the run loop (and
// refused, not waited on, when the loop already holds a run's worth).
func subscribeRuns(ctx context.Context, nc *nats.Conn, node string, runner *collection.Runner, jobs chan<- runJob) (*nats.Subscription, error) {
	return nc.Subscribe(collection.RunSubject(node), func(m *nats.Msg) {
		respond := func(r collection.RunReply) {
			b, err := json.Marshal(r)
			if err != nil {
				b, _ = json.Marshal(collection.RunReply{Error: "the node could not encode its answer"})
			}
			_ = m.Respond(b)
		}
		var req collection.RunRequest
		if err := json.Unmarshal(m.Data, &req); err != nil {
			respond(collection.RunReply{Error: "the node could not read the run request"})
			return
		}
		timeout := time.Duration(req.TimeoutMs) * time.Millisecond
		if timeout <= 0 {
			timeout = defaultRunTimeout
		}
		switch req.Kind {
		case collection.RunPreview:
			go func() { respond(previewRun(ctx, runner, req.Tasks, req.Mask, timeout)) }()
		case collection.RunNow:
			if len(req.Tasks) != 1 {
				respond(collection.RunReply{Error: "a run now runs exactly one task"})
				return
			}
			select {
			case jobs <- runJob{task: req.Tasks[0], deadline: time.Now().Add(timeout), respond: respond}:
			default:
				respond(collection.RunReply{Error: "the node is already running a run now; retry in a moment"})
			}
		default:
			respond(collection.RunReply{Error: fmt.Sprintf("the node does not know a run of kind %q", req.Kind)})
		}
	})
}

// previewRun runs each preview task once, within the budget, and answers with
// what each read. Nothing is published, so nothing is stored.
func previewRun(ctx context.Context, runner *collection.Runner, tasks []collection.TaskSpec, mask []string, timeout time.Duration) collection.RunReply {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	reply := collection.RunReply{Results: make([]collection.RunResult, 0, len(tasks))}
	for _, task := range tasks {
		start := time.Now()
		res := collection.RunResult{TaskID: task.ID, Samples: []collection.RunSample{}}
		if fn, err := driver.ParseBaked(task.Spec); err == nil {
			res.Function = fn.Function
		}
		dps, faults, excerpt, cut := collectPreviewTask(ctx, runner, task, mask)
		for _, s := range dps {
			res.Samples = append(res.Samples, collection.RunSampleOf(s))
		}
		for _, f := range faults {
			res.Faults = append(res.Faults, f.Error())
		}
		res.Excerpt, res.ExcerptTruncated = excerpt, cut
		res.TookMs = time.Since(start).Milliseconds()
		reply.Results = append(reply.Results, res)
	}
	slog.Info("preview ran", "facility", "collection", "tasks", len(tasks))
	return reply
}

// collectPreviewTask runs one preview poll with nothing an endpoint holds: no
// held session, no learned http auth (a digest challenge is answered afresh,
// a login logs in once), and a tls pin trusted on first use. A poll that
// needs a held session or a bound udp socket faults naming why (the server
// refuses such a preview before it asks, so this is the node's own guard).
// The excerpt is what the fetch answered, before any emit read it.
func collectPreviewTask(ctx context.Context, runner *collection.Runner, task collection.TaskSpec, mask []string) (dps []collection.Sample, faults []error, excerpt string, cut bool) {
	fn, err := driver.ParseBaked(task.Spec)
	if err != nil {
		return nil, []error{fmt.Errorf("node: preview task %s: %w", task.ID, err)}, "", false
	}
	var p endpointParams
	if len(task.EndpointParams) > 0 {
		if err := json.Unmarshal(task.EndpointParams, &p); err != nil {
			return nil, []error{fmt.Errorf("node: bad preview params for task %s: %w", task.ID, err)}, "", false
		}
	}
	switch {
	case task.Transport == "udp":
		return nil, []error{fmt.Errorf("node: a preview has no bound udp socket; attach the endpoint and run its poll now instead")}, "", false
	case !driver.IsHTTP(task.Transport) && task.Session != nil:
		return nil, []error{fmt.Errorf("node: a %s poll whose driver declares a session rides a held session, which a preview does not open", task.Transport)}, "", false
	}
	// The excerpt is scrubbed here, where the credential is known, in every
	// form an answer echoes it (a basic header's value too), and only then cut,
	// so a secret straddling the cut leaves no prefix behind.
	cred, _ := driver.ResolveHTTPCredential(task.Session, task.Secrets)
	pt := collection.DriverPollTask{
		Fn:        fn,
		Transport: task.Transport,
		Target:    p.Target,
		Secrets:   task.Secrets,
		Auth:      task.Session,
		Capture: func(payload driver.Payload) {
			excerpt, cut = previewExcerpt(payload, cred, mask)
		},
	}
	if p.Timeout != "" {
		if d, err := time.ParseDuration(p.Timeout); err == nil {
			pt.Timeout = d
		}
	}
	if task.Transport == "https" {
		trust, err := tlsTrustOf(p)
		if err != nil {
			return nil, []error{fmt.Errorf("node: preview task %s: %w", task.ID, err)}, "", false
		}
		pt.TLS = &trust
	}
	dps, faults = runner.CollectDriverPoll(ctx, pt)
	return dps, faults, excerpt, cut
}

// previewExcerpt renders what a fetch answered, scrubbed of the credential
// and every secret field value before it is cut to the excerpt's bound.
func previewExcerpt(payload driver.Payload, cred *driver.HTTPCredential, secretValues []string) (string, bool) {
	full, _ := collection.PayloadExcerpt(payload, int(^uint(0)>>1))
	return collection.PayloadExcerpt(driver.Payload{Text: collection.Scrub(full, cred, secretValues...)}, collection.ExcerptLimit)
}

// landableRun is what a run now publishes: its samples held to the #1145
// change memory a scheduled run keeps (an event or a log line that has not
// changed since the last run, either kind, is not landed again). The caller
// commits the landing to that memory once it has published, as a scheduled
// run does, so a landing that failed to publish is offered again. Metrics and
// properties pass whole.
func landableRun(sched *pollSchedule, taskID string, dps []collection.Sample) []collection.Sample {
	return sched.changed(taskID, dps)
}

// runNowTask runs one of the node's poll tasks now, on the run loop, exactly
// as a scheduled run would, its schedule untouched: the endpoint's held
// session borrowed where it has one, its gate honored (a gated stateless
// poll is refused with the gate's refusal and nothing is sent), a terminal
// refusal it meets gating the endpoint, and what it reads published (its
// faults as collection-failed, its samples as a batch), so it lands.
func runNowTask(ctx context.Context, out *telemetrySpool, node string, runner *collection.Runner, sessions *sessionManager, sched *pollSchedule, task collection.TaskSpec, deadline time.Time) collection.RunReply {
	if task.Mode != "poll" || !driver.IsBaked(task.Spec) {
		return collection.RunReply{Error: "a run now runs a driver poll, and this task is not one"}
	}
	if !time.Now().Before(deadline) {
		return collection.RunReply{Error: "the run's budget passed before the node could take it"}
	}
	if driver.IsHTTP(task.Transport) && sessions.streamPending(task.EndpointID) {
		// The endpoint's stream is making its first attempt under this
		// fingerprint (#1038): a poll would send the credential beside it.
		return collection.RunReply{Pending: true, Error: "the endpoint's stream is still making its first attempt; run the poll again once it has"}
	}
	ownRequest := sessions.lookup(task.EndpointID) == nil || driver.IsHTTP(task.Transport)
	if gt, gated := sessions.blocked(task.EndpointID, statelessRung(task)); gated && ownRequest {
		return collection.RunReply{Gated: true, Error: gt.refusal().Error()}
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	start := time.Now()
	res := collection.RunResult{TaskID: task.ID, Samples: []collection.RunSample{}}
	if fn, err := driver.ParseBaked(task.Spec); err == nil {
		res.Function = fn.Function
	}
	dps, faults := collectDriverTask(ctx, runner, task, sessions)
	if ownRequest {
		for _, f := range faults {
			if terr, ok := collection.AsTerminal(f); ok {
				sessions.refuseOver(task.Transport, task.EndpointID, task.Fingerprint, terr)
			}
		}
	}
	for _, f := range faults {
		res.Faults = append(res.Faults, f.Error())
		if err := publishCollectionFailed(out, node, task.ID, f); err != nil {
			return collection.RunReply{Error: "the node could not publish the run's faults: " + err.Error()}
		}
	}
	if landed := landableRun(sched, task.ID, dps); len(landed) > 0 {
		if err := publishBatch(out, node, task.ID, landed); err != nil {
			return collection.RunReply{Error: "the node could not publish the run's samples: " + err.Error()}
		}
		sched.commitChanged(task.ID, landed)
	}
	for _, s := range dps {
		res.Samples = append(res.Samples, collection.RunSampleOf(s))
	}
	res.TookMs = time.Since(start).Milliseconds()
	slog.Info("poll ran now", "facility", "collection", "task", task.ID, "samples", len(dps), "faults", len(faults))
	return collection.RunReply{Results: []collection.RunResult{res}}
}
