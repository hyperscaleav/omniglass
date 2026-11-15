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
// the node to run work now and the node answers once. Both kinds are
// operator work on the endpoint lanes (#1249, ADR-1249). A run now runs one
// of the node's own poll tasks on its endpoint's lane, ahead of the
// scheduled runs waiting there, so it borrows the endpoint's session, honors
// its gate exactly as a scheduled run does, and lands what it reads. A
// preview runs polls of a spec no endpoint holds yet, touching no held
// session and publishing nothing: on the lane of the inventoried endpoint
// its transport and target name, so it never talks to a device beside that
// endpoint's own work, and off-lane when it names none.

// defaultRunTimeout bounds a run whose request names no budget.
const defaultRunTimeout = 20 * time.Second

// lanesFull is the answer to operator work the lanes refused at the operator
// queue's cap.
const lanesFull = "the node's operator lanes are full; retry in a moment"

// runRoute is what a run request needs on the node: where a run now lands
// what it reads, what it borrows and books, and the lanes both kinds take.
type runRoute struct {
	out      *telemetrySpool
	node     string
	runner   *collection.Runner
	sessions *sessionManager
	sched    *pollSchedule
	report   *collectionReport
	pool     *lanePool
}

// subscribeRuns takes the server's run requests for this node.
func subscribeRuns(ctx context.Context, nc *nats.Conn, rt runRoute) (*nats.Subscription, error) {
	return nc.Subscribe(collection.RunSubject(rt.node), func(m *nats.Msg) {
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
		takeRun(ctx, rt, req, respond)
	})
}

// takeRun routes one run request onto its lane. A closed gate answers at
// once, as the request arrives (#1650): queued, it would wait out whatever
// its lane is in the middle of. The budget is stamped here, so a run its
// lane takes late runs only in what remains of it, and one whose budget is
// gone answers unrun. Operator work past the operator queue's cap is
// refused at once, never queued.
func takeRun(ctx context.Context, rt runRoute, req collection.RunRequest, respond func(collection.RunReply)) {
	if ctx.Err() != nil {
		respond(collection.RunReply{Error: "the node is stopping"})
		return
	}
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultRunTimeout
	}
	deadline := time.Now().Add(timeout)
	g := rt.sessions.gatesOf()
	switch req.Kind {
	case collection.RunPreview:
		if reply, closed := previewGate(g, req.Tasks); closed {
			respond(reply)
			return
		}
		run := func() {
			left := time.Until(deadline)
			if left <= 0 {
				respond(collection.RunReply{Error: "the run's budget passed before the node could take it"})
				return
			}
			respond(previewRun(ctx, rt.runner, g, req.Tasks, req.Mask, left))
		}
		ep := g.resolve(req.Tasks)
		if ep == "" {
			go run()
			return
		}
		if !rt.pool.offer(ep, laneOperator, run) {
			respond(collection.RunReply{Full: true, Error: lanesFull})
		}
	case collection.RunNow:
		if len(req.Tasks) != 1 {
			respond(collection.RunReply{Error: "a run now runs exactly one task"})
			return
		}
		task := req.Tasks[0]
		if c, closed := rt.sessions.consult(task.EndpointID, statelessRung(task), sendsOwnRequest(rt.sessions, task)); closed {
			respond(runReply(c))
			return
		}
		ok := rt.pool.offer(laneKey(task), laneOperator, func() {
			respond(runNowTask(ctx, rt.out, rt.node, rt.runner, rt.sessions, rt.sched, rt.report, task, deadline))
		})
		if !ok {
			respond(collection.RunReply{Full: true, Error: lanesFull})
		}
	default:
		respond(collection.RunReply{Error: fmt.Sprintf("the node does not know a run of kind %q", req.Kind)})
	}
}

// previewGate is the answer to a preview a closed gate holds: the gate of
// the endpoint it resolves to (any reason, a refusal included, since a
// preview sends its own request), else a host-wide reason held on any
// endpoint at its target's host (#1650), which holds every transport to that
// host. false when nothing holds it.
func previewGate(g *gates, tasks []collection.TaskSpec) (collection.RunReply, bool) {
	if c, target, closed := g.previewClosed(tasks); closed {
		return previewReply(c, target), true
	}
	for _, task := range tasks {
		if r, closed := g.closedHost(taskTarget(task)); closed {
			return collection.RunReply{Gated: true, GateReason: string(r.kind),
				Error: "node: the host is " + r.text + ", so the preview was not sent; " + reasonRules[r.kind].release}, true
		}
	}
	return collection.RunReply{}, false
}

// previewRun runs each preview task once, within the budget, and answers with
// what each read. Nothing is published, so nothing is stored. The gate is
// asked again here, as the preview's lane takes it: a request ahead of it
// on the lane may have closed it since it arrived.
func previewRun(ctx context.Context, runner *collection.Runner, g *gates, tasks []collection.TaskSpec, mask []string, timeout time.Duration) collection.RunReply {
	if reply, closed := previewGate(g, tasks); closed {
		return reply
	}
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
// run does (from the run's whole sample set), so a landing that failed to
// publish is offered again. Metrics and
// properties pass whole.
func landableRun(sched *pollSchedule, taskID string, dps []collection.Sample) []collection.Sample {
	return sched.changed(taskID, dps)
}

// heldRun is the answer to a run now on an endpoint a reason holds whole
// (unreachable, disrupted): gated, naming the reason and what releases it.
func heldRun(r reason) collection.RunReply {
	return collection.RunReply{Gated: true, GateReason: string(r.kind),
		Error: "node: the endpoint is " + r.text + "; " + reasonRules[r.kind].release}
}

// runNowTask runs one of the node's poll tasks now, on its lane, exactly
// as a scheduled run would, its schedule untouched: the endpoint's held
// session borrowed where it has one, its gate honored (a gated stateless
// poll is refused with the gate's refusal, an endpoint its probe reads
// unreachable is answered with that, and nothing is sent), a terminal
// refusal it meets gating the endpoint, and what it reads published (its
// faults as collection-failed, its samples as a batch), so it lands.
func runNowTask(ctx context.Context, out *telemetrySpool, node string, runner *collection.Runner, sessions *sessionManager, sched *pollSchedule, report *collectionReport, task collection.TaskSpec, deadline time.Time) collection.RunReply {
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
	ownRequest := sendsOwnRequest(sessions, task)
	// takeRun answered a gate closed when the request arrived; one that
	// closed since (a reboot ahead of it on the lane) is answered here, and
	// nothing is sent.
	if c, closed := sessions.consult(task.EndpointID, statelessRung(task), ownRequest); closed {
		return runReply(c)
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	start := time.Now()
	res := collection.RunResult{TaskID: task.ID, Samples: []collection.RunSample{}}
	if fn, err := driver.ParseBaked(task.Spec); err == nil {
		res.Function = fn.Function
	}
	var dps []collection.Sample
	var faults []error
	cost := measureRun(func() { dps, faults = collectDriverTask(ctx, runner, task, sessions) })
	// A run now is a poll run like a scheduled one, so it counts in the
	// endpoint's collection report (#1369), its CPU time too (#1870).
	report.poll(task.EndpointID, cost, !landedData(dps) && len(faults) > 0)
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
	}
	sched.commitChanged(task.ID, dps)
	for _, s := range dps {
		res.Samples = append(res.Samples, collection.RunSampleOf(s))
	}
	res.TookMs = time.Since(start).Milliseconds()
	slog.Info("poll ran now", "facility", "collection", "task", task.ID, "samples", len(dps), "faults", len(faults))
	return collection.RunReply{Results: []collection.RunResult{res}}
}
