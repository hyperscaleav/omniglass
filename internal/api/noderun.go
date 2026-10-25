package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
	"github.com/hyperscaleav/omniglass/internal/redact"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The on-demand node run (#1163, ADR-1163), and the validate route beside it:
// check a driver spec as a save would without saving it; preview a spec's
// polls once through a node against a target, storing nothing; run an attached
// endpoint's poll now, landing its samples. Preview and run now ask a node and
// wait for its one answer, so both are bounded by a stricter class than the
// API's own (#962 is the API-wide limit): a few in flight across the server and
// fewer per caller, a run past either refused before anything is asked.

// NodeRunner is the server's line to its nodes for on-demand runs: the bus
// asks the node named and returns its answer.
type NodeRunner interface {
	RunPreview(ctx context.Context, node string, tasks []storage.WorklistTask, timeout time.Duration) (collection.RunReply, error)
	RunTaskNow(ctx context.Context, node, taskID string, timeout time.Duration) (collection.RunReply, error)
}

// WithNodeRunner supplies the bus seam previews and runs now go through.
// Without it both routes answer 503: nothing can reach a node.
func WithNodeRunner(r NodeRunner) Option { return func(o *options) { o.nodeRunner = r } }

// WithNodeRunLimits bounds how many on-demand runs may be in flight at once,
// in all and per principal. Zero keeps a default.
func WithNodeRunLimits(total, perPrincipal int) Option {
	return func(o *options) { o.nodeRuns, o.nodeRunsPerPrincipal = total, perPrincipal }
}

// The stricter class: an on-demand run holds a request open while a node
// works, and it reaches the fleet, so fewer run at once than commands wait.
const (
	defaultNodeRuns             = 16
	defaultNodeRunsPerPrincipal = 2
	// defaultRunSeconds is a run's budget when the caller names none; the
	// node's whole answer, every poll included, must fit in it.
	defaultRunSeconds = 15
)

type validateDriverInput struct {
	Body struct {
		Spec json.RawMessage `json:"spec" doc:"The driver spec to check, as create-driver takes it"`
	}
}

type validateDriverOutput struct {
	Body struct {
		Valid    bool             `json:"valid" doc:"Whether a save would accept the spec"`
		Findings []driver.Finding `json:"findings" doc:"Every fault the validator can pin down, the first the one a save names; empty when valid"`
	}
}

type previewDriverInput struct {
	Body struct {
		Node           string            `json:"node" doc:"The node that runs the preview, by name: the one that reaches the target" example:"edge-hq"`
		Driver         string            `json:"driver,omitempty" doc:"A stored driver to preview, by name; or give spec"`
		Spec           json.RawMessage   `json:"spec,omitempty" doc:"A driver spec to preview inline, unsaved; or give driver"`
		Inputs         map[string]string `json:"inputs,omitempty" doc:"The inputs an attach would take (host, port, a secret by name); a secret must be one you may reveal, since a preview hands it to the target"`
		Polls          []string          `json:"polls,omitempty" doc:"The polls to run, by name; absent runs every poll"`
		TimeoutSeconds int               `json:"timeout_seconds,omitempty" minimum:"1" maximum:"30" doc:"How long the node may take for the whole preview (default 15, at most 30)"`
	}
}

type pollRunBody struct {
	Poll             string                 `json:"poll" doc:"The poll that ran"`
	Samples          []collection.RunSample `json:"samples" doc:"What its emits extracted"`
	Faults           []string               `json:"faults,omitempty" doc:"Each emit that did not locate or type, or the fetch that failed"`
	Excerpt          string                 `json:"excerpt,omitempty" doc:"What the fetch answered, before any emit read it, bounded and masked of every secret the preview used"`
	ExcerptTruncated bool                   `json:"excerpt_truncated,omitempty" doc:"The answer was longer than the excerpt"`
	TookMs           int64                  `json:"took_ms" doc:"How long the poll took on the node"`
}

type previewDriverOutput struct {
	Body struct {
		Node    string        `json:"node" doc:"The node that ran it"`
		Driver  string        `json:"driver,omitempty" doc:"The stored driver previewed; absent for an inline spec"`
		Target  string        `json:"target" doc:"Where the polls were sent"`
		Stored  bool          `json:"stored" doc:"Always false: a preview stores no sample"`
		Results []pollRunBody `json:"results" doc:"One per poll"`
	}
}

type runEndpointPollInput struct {
	Endpoint       string `path:"endpoint" doc:"The endpoint, as <component>/<endpoint name> or its id" example:"hq.west.level-2.huddle.$comp.panel-1/telnet"`
	Poll           string `path:"poll" doc:"The poll to run, by its function name" example:"status"`
	TimeoutSeconds int    `query:"timeout_seconds" minimum:"1" maximum:"30" doc:"How long the node may take (default 15, at most 30)"`
}

type runEndpointPollOutput struct {
	Body struct {
		Endpoint  string                 `json:"endpoint" doc:"The endpoint, by name"`
		Component string                 `json:"component,omitempty" doc:"Its component, by name"`
		Node      string                 `json:"node" doc:"The node that ran the poll"`
		Stored    bool                   `json:"stored" doc:"Always true: a run now lands its samples as a scheduled run does"`
		Poll      string                 `json:"poll" doc:"The poll that ran"`
		Samples   []collection.RunSample `json:"samples" doc:"What its emits extracted, landed as a scheduled run's are"`
		Faults    []string               `json:"faults,omitempty" doc:"Each emit that did not locate or type, or the fetch that failed, each landed as collection-failed"`
		TookMs    int64                  `json:"took_ms" doc:"How long the poll took on the node"`
	}
}

func registerNodeRunRoutes(api huma.API, a *authenticator, gw storage.Gateway, runner NodeRunner, runs *waitLimiter) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "validate-driver-spec",
		Extensions:  mcpExposed(),
		Method:      http.MethodPost,
		Path:        "/drivers:validate",
		Summary:     "Check a driver spec without saving it",
		Description: "Checks a driver spec by the rules a save applies (the strict parse, then validation against the live catalogs) and writes nothing. The answer lists every fault the validator can pin down, each pinned on the function it is in, the first always the fault a save would name; an empty list is a spec create-driver accepts. Gated by driver:read.",
	}, "driver", "read"), func(ctx context.Context, in *validateDriverInput) (*validateDriverOutput, error) {
		if len(in.Body.Spec) == 0 {
			return nil, huma.Error422UnprocessableEntity("spec is required")
		}
		findings, err := gw.DriverSpecFindings(ctx, in.Body.Spec)
		if err != nil {
			return nil, huma.Error500InternalServerError("validate spec")
		}
		out := &validateDriverOutput{}
		out.Body.Valid = len(findings) == 0
		out.Body.Findings = findings
		if out.Body.Findings == nil {
			out.Body.Findings = []driver.Finding{}
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "preview-driver",
		// A poll's request is whatever its spec says (a POST, any line), so a
		// preview can change the component it reaches: not read-only, and
		// destructive, though it stores nothing in omniglass.
		Extensions: mcpExposed(tooldef.Hints{Destructive: true, OpenWorld: true}),
		Method:     http.MethodPost,
		Path:       "/drivers:preview",
		Summary:    "Preview a driver spec's polls through a node",
		Description: "Runs a driver spec's polls once through the node named, against the target its inputs name, and answers with what each poll read: the samples its emits extracted, each emit that did not locate, and an excerpt of the raw answer, masked of every secret the preview used. Nothing is stored. The spec is a stored driver (driver) or an unsaved one (spec); an invalid spec is a 422 listing its findings, as validate-driver-spec gives them. A preview runs a poll that is its own request (http, https, an snmp get, a tcp line); a poll that rides a held session or a bound socket needs an attached endpoint (run-endpoint-poll). " +
			"A secret input must be one the caller may reveal (secret:reveal), since the preview hands it to the target named. A node that is not connected is a 503, one that does not answer in time a 504; too many runs in flight is a 429. Audited on the node and on each secret used. Gated by driver:preview.",
	}, "driver", "preview"), func(ctx context.Context, in *previewDriverInput) (*previewDriverOutput, error) {
		if runner == nil {
			return nil, huma.Error503ServiceUnavailable("no bus is wired, so no node can run a preview")
		}
		if (in.Body.Driver == "") == (len(in.Body.Spec) == 0) {
			return nil, huma.Error422UnprocessableEntity("give exactly one of driver (a stored driver, by name) or spec (an unsaved spec)")
		}
		if in.Body.Node == "" {
			return nil, huma.Error422UnprocessableEntity("node is required: the node that reaches the target runs the preview")
		}
		principal := actorID(ctx)
		if !runs.acquire(principal) {
			return nil, huma.NewError(http.StatusTooManyRequests, "too many on-demand runs are in flight; retry in a moment")
		}
		defer runs.release(principal)
		plan, err := gw.PlanDriverPreview(ctx, principal, storage.PreviewInput{
			Node: in.Body.Node, Driver: in.Body.Driver, Spec: in.Body.Spec, Inputs: in.Body.Inputs, Polls: in.Body.Polls,
			NodeRead:     a.scopeFor(ctx, "node", "read"),
			SecretReveal: a.scopeFor(ctx, "secret", "reveal"),
			CanAdmin:     a.canSecretAdmin(ctx, "reveal"),
		})
		if err != nil {
			return nil, mapNodeRunErr(err)
		}
		reply, err := runner.RunPreview(ctx, plan.NodeName, plan.Tasks, runBudget(in.Body.TimeoutSeconds))
		if err != nil {
			return nil, mapNodeRunErr(err)
		}
		if reply.Error != "" {
			return nil, huma.Error502BadGateway("the node could not run the preview: " + redact.Secrets(reply.Error, plan.Sealed...))
		}
		out := &previewDriverOutput{}
		out.Body.Node, out.Body.Driver, out.Body.Target = plan.NodeName, plan.Driver, plan.Target
		out.Body.Results = make([]pollRunBody, 0, len(reply.Results))
		for _, r := range reply.Results {
			out.Body.Results = append(out.Body.Results, pollRunOf(r, plan.Sealed))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "run-endpoint-poll",
		Extensions:  mcpExposed(tooldef.Hints{OpenWorld: true}),
		Method:      http.MethodPost,
		Path:        "/endpoints/{endpoint}/polls/{poll}:run",
		Summary:     "Run an endpoint's poll now",
		Description: "Runs one of an attached endpoint's polls now, on the node that holds the endpoint, outside its schedule (which is unchanged), and answers with what it read. The samples land as a scheduled run's do. The run borrows the endpoint's held session where it has one and honors its gate: a gated endpoint refuses the run with the gate's reason (409) and nothing is sent. A poll the endpoint does not have is a 404 naming the polls it has; an endpoint on no node is a 409. A node that is not connected is a 503, one that does not answer in time a 504; too many runs in flight is a 429. " +
			"Audited on the endpoint. Gated by endpoint:run; read and run scopes (through the component) drive the 404 versus 403 split.",
	}, "endpoint", "run"), func(ctx context.Context, in *runEndpointPollInput) (*runEndpointPollOutput, error) {
		if runner == nil {
			return nil, huma.Error503ServiceUnavailable("no bus is wired, so no node can run a poll now")
		}
		principal := actorID(ctx)
		if !runs.acquire(principal) {
			return nil, huma.NewError(http.StatusTooManyRequests, "too many on-demand runs are in flight; retry in a moment")
		}
		defer runs.release(principal)
		run, err := gw.PlanEndpointRun(ctx, principal, in.Endpoint, in.Poll,
			a.scopeFor(ctx, "endpoint", "read"), a.scopeFor(ctx, "endpoint", "run"))
		if err != nil {
			return nil, mapNodeRunErr(err)
		}
		reply, err := runner.RunTaskNow(ctx, run.NodeName, run.TaskID, runBudget(in.TimeoutSeconds))
		if err != nil {
			return nil, mapNodeRunErr(err)
		}
		switch {
		case reply.Gated:
			return nil, huma.Error409Conflict("the endpoint is gated, so the poll was not sent: " + reply.Error + "; fix what refused it, or retry the endpoint (retry-endpoint)")
		case reply.Error != "":
			return nil, huma.Error502BadGateway("the node could not run the poll: " + reply.Error)
		case len(reply.Results) != 1:
			return nil, huma.Error502BadGateway("the node answered the run without its result")
		}
		out := &runEndpointPollOutput{}
		out.Body.Endpoint, out.Body.Component, out.Body.Node, out.Body.Stored = run.Endpoint, run.Component, run.NodeName, true
		res := pollRunOf(reply.Results[0], nil)
		out.Body.Poll, out.Body.Samples, out.Body.Faults, out.Body.TookMs = res.Poll, res.Samples, res.Faults, res.TookMs
		return out, nil
	})
}

// newRunLimiter is the stricter class's limiter: the command waits' counter,
// with the on-demand run's own defaults.
func newRunLimiter(total, per int) *waitLimiter {
	if total <= 0 {
		total = defaultNodeRuns
	}
	if per <= 0 {
		per = defaultNodeRunsPerPrincipal
	}
	return newWaitLimiter(total, per)
}

// runBudget is a run's budget: the caller's, else the default.
func runBudget(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = defaultRunSeconds
	}
	return time.Duration(seconds) * time.Second
}

// pollRunOf maps a node's result for one poll onto the answer, masked of the
// secrets given wherever the answer could echo them.
func pollRunOf(r collection.RunResult, sealed []string) pollRunBody {
	b := pollRunBody{Poll: r.Function, Samples: r.Samples, TookMs: r.TookMs,
		Excerpt: redact.Secrets(r.Excerpt, sealed...), ExcerptTruncated: r.ExcerptTruncated}
	if b.Samples == nil {
		b.Samples = []collection.RunSample{}
	}
	for i := range b.Samples {
		b.Samples[i].Text = redact.Secrets(b.Samples[i].Text, sealed...)
	}
	for _, f := range r.Faults {
		b.Faults = append(b.Faults, redact.Secrets(f, sealed...))
	}
	return b
}

// mapNodeRunErr maps the planning and the node's refusals onto the answer.
func mapNodeRunErr(err error) error {
	var findings *storage.SpecFindingsError
	if errors.As(err, &findings) {
		details := make([]error, 0, len(findings.Findings))
		for _, f := range findings.Findings {
			loc := "body.spec"
			if f.Function != "" {
				loc += " (" + f.Kind + " " + f.Function + ")"
			}
			details = append(details, &huma.ErrorDetail{Message: f.Message, Location: loc})
		}
		return huma.Error422UnprocessableEntity("the spec does not validate: "+findings.Findings[0].Message, details...)
	}
	switch {
	case errors.Is(err, collection.ErrNodeAbsent):
		return huma.Error503ServiceUnavailable("the node is not connected, so it cannot run this now")
	case errors.Is(err, collection.ErrNodeTimeout):
		return huma.Error504GatewayTimeout("the node did not answer within the run's budget")
	case errors.Is(err, collection.ErrTaskNotOnWorklist):
		return huma.Error409Conflict("the poll is not on its node's worklist (disabled, or moved); read the endpoint and retry")
	case errors.Is(err, storage.ErrPreviewUnsupported), errors.Is(err, storage.ErrAttachInvalid):
		return huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), "storage: "))
	case errors.Is(err, storage.ErrNoSuchPoll):
		return huma.Error404NotFound(strings.TrimPrefix(strings.TrimPrefix(err.Error(), "storage: "), "no such poll: "))
	case errors.Is(err, storage.ErrEndpointUnplaced):
		return huma.Error409Conflict(strings.TrimPrefix(strings.TrimPrefix(err.Error(), "storage: "), "endpoint unplaced: "))
	case errors.Is(err, storage.ErrNodeNotFound):
		return huma.Error404NotFound("node not found")
	case errors.Is(err, storage.ErrTypeNotFound):
		return huma.Error404NotFound("driver not found")
	}
	return mapEndpointErr(err)
}
