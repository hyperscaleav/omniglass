package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/assistant"
	"github.com/hyperscaleav/omniglass/internal/assistant/agui"
	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/egress"
	mcpserver "github.com/hyperscaleav/omniglass/internal/mcp"
	"github.com/hyperscaleav/omniglass/internal/mcp/skills"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A server-side omnibot run (#2121, ADR-2121): the person's prompt runs on the
// server as their omnibot, over one of their stored conversations, and is
// narrated back as AG-UI events. The run holds a token of its own, an omnibot
// agent token minted for the person, revoked when the run ends, and calls its
// tools through the same router a direct call takes; the model never sees it.
// A write the person's agent policy asks about ends the run on an interrupt,
// kept as the conversation's pending call; a resume naming that call answers
// it. The stored transcript is the conversation: the history a client sends
// is not believed. A run claims its conversation before any tool runs, so two
// runs, two resumes of one waiting call included, never act on it at once.

// runLifetime bounds a run: its deadline, its claim on the conversation, and
// its own token should the revoke never land. A run that died frees its
// conversation when the claim lapses.
const runLifetime = 30 * time.Minute

// modelTimeout bounds one model turn: a live model takes far longer than the
// egress contract's default for a webhook.
const modelTimeout = 2 * time.Minute

type aguiMessageInput struct {
	_       struct{}        `additionalProperties:"true"`
	ID      string          `json:"id,omitempty" required:"false"`
	Role    string          `json:"role" doc:"Who said it; only the last user message is read"`
	Content json.RawMessage `json:"content,omitempty" required:"false" doc:"Text, or an array of parts whose text parts are read"`
}

type aguiResumeInput struct {
	_           struct{}        `additionalProperties:"true"`
	InterruptID string          `json:"interruptId" doc:"The interrupt being answered: the call the run waits on"`
	Status      string          `json:"status" enum:"resolved,cancelled" doc:"resolved answers it with the payload; cancelled declines it"`
	Payload     json.RawMessage `json:"payload,omitempty" required:"false" doc:"The answer, {\"approved\": true} to run the call"`
}

// aguiContextInput is one AG-UI context entry: what the person handed over
// with the prompt (#2122), a reference or a page query, read as data.
type aguiContextInput struct {
	_           struct{} `additionalProperties:"true"`
	Description string   `json:"description" maxLength:"100" doc:"What it is: a kind, or page"`
	Value       string   `json:"value" maxLength:"2000" doc:"A name or dotted address, or a route and its query; never a rendered value"`
}

// aguiSkillInput is an embedded skill the run carries out (#2122): its
// prompt, with the arguments given, is what the model is sent.
type aguiSkillInput struct {
	_         struct{}          `additionalProperties:"true"`
	Name      string            `json:"name" maxLength:"100" doc:"The skill, as omniglass mcp skill lists it"`
	Arguments map[string]string `json:"arguments,omitempty" required:"false" doc:"The skill's arguments, by name"`
}

type aguiForwardedInput struct {
	_     struct{}        `additionalProperties:"true"`
	Skill *aguiSkillInput `json:"skill,omitempty" required:"false" doc:"An embedded skill to run, its prompt ahead of the person's message"`
}

type runAssistantConversationInput struct {
	Name string `path:"name" doc:"Your conversation's name"`
	Body struct {
		_        struct{}           `additionalProperties:"true"`
		ThreadID string             `json:"threadId,omitempty" required:"false" doc:"Ignored: the conversation is the thread"`
		RunID    string             `json:"runId,omitempty" required:"false" doc:"The run's id, echoed in its events"`
		Messages []aguiMessageInput `json:"messages,omitempty" required:"false" doc:"The client's view of the conversation; only its last user message is read, as the prompt"`
		Resume   []aguiResumeInput  `json:"resume,omitempty" required:"false" doc:"The answer to the interrupt the last run ended on"`
		Context  []aguiContextInput `json:"context,omitempty" required:"false" maxItems:"20" doc:"What the person handed over with the prompt, read as data ahead of it; ignored on a resume"`
		Forward  aguiForwardedInput `json:"forwardedProps,omitempty" required:"false" doc:"An embedded skill to run; ignored on a resume"`
	}
}

func registerAssistantRunRoute(api huma.API, a *authenticator, gw storage.Gateway, o options) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "run-assistant-conversation",
		Method:      http.MethodPost,
		Path:        "/assistant/conversations/{name}:run",
		Summary:     "Run omnibot on one of your conversations",
		Description: "Runs your prompt (the last user message) as your omnibot, over the stored conversation, and streams the run as AG-UI events (text/event-stream). " +
			"A call your agent policy asks about ends the run on an interrupt naming it; run again with resume to answer it. " +
			"A run that reaches its step limit without an answer finishes with the result {\"stopped\": \"step_limit\", \"steps\": N}. " +
			"The run's tool calls are audited as omnibot's, on your behalf. Only you, as yourself, run your conversations: anyone else's is a 404, " +
			"and an agent or an impersonation session is a 403. A resume that names no waiting call, a prompt while one waits, or a run while another holds the conversation is a 409. " +
			"Context entries the person hands over with a prompt (references and the page, at most 20) are framed as data ahead of it. " +
			"forwardedProps.skill names an embedded skill whose prompt, with its arguments, the run carries out; an unknown skill or a missing argument is a 422. " +
			"The run talks to the model the assistant settings configure, read as it starts; with none, the server's recording, if it has one. " +
			"A recording answers only the prompts it was recorded for: any other question is a 422 (type urn:omniglass:omnibot-replay-unmatched) that touches nothing; the list route names the prompts. " +
			"A server with no model, or a configured model whose key no longer resolves, is a 503. Gated by assistant:use.",
	}, "assistant", "use"), func(ctx context.Context, in *runAssistantConversationInput) (*huma.StreamResponse, error) {
		u, ok := userFrom(ctx)
		if !ok || u.Person == nil || u.Agent != nil || impersonationMode(ctx) != "" {
			return nil, huma.Error403Forbidden("only a person, as themselves, runs omnibot")
		}
		model, err := runProvider(ctx, gw, o)
		if err != nil {
			return nil, err
		}
		conv, ms, err := gw.GetAssistantConversation(ctx, callerID(ctx), in.Name)
		if err != nil {
			return nil, mapAssistantErr(err)
		}
		history, err := runHistory(ms)
		if err != nil {
			return nil, huma.Error500InternalServerError("read the conversation")
		}
		prompt := lastUserText(in.Body.Messages)
		if sk := in.Body.Forward.Skill; sk != nil && len(in.Body.Resume) == 0 {
			if prompt, err = skillPrompt(*sk, prompt); err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
		}
		if len(in.Body.Resume) == 0 && prompt == "" {
			return nil, huma.Error422UnprocessableEntity("a run needs a user message or a resume")
		}
		// A recording answers only what it was recorded for: any other
		// question is refused here, before the conversation is claimed or
		// touched, rather than played an unrelated run (#2443).
		if sc, ok := model.(provider.Scripted); ok && len(in.Body.Resume) == 0 && !sc.Plays(prompt) {
			return nil, replayUnmatched()
		}

		tok, hash, prefix, err := auth.NewBearerToken()
		if err != nil {
			return nil, huma.Error500InternalServerError("start the run")
		}
		exp := time.Now().Add(runLifetime)
		claim := storage.AssistantRunClaim{Run: rand.Text(), Until: exp}
		if len(ms) > 0 {
			claim.After = ms[len(ms)-1].Seq
		}
		if err := gw.ClaimAssistantRun(ctx, callerID(ctx), conv.Name, claim); err != nil {
			return nil, mapAssistantErr(err)
		}
		release := func() {
			if err := gw.ReleaseAssistantRun(context.WithoutCancel(ctx), callerID(ctx), conv.Name, claim.Run); err != nil {
				o.log.Error("omnibot run: release the conversation", "err", err)
			}
		}
		ua, ip := clientMetaFrom(ctx)
		issued, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{
			ActorID: actorID(ctx), Username: u.Person.Username, SecretHash: hash, Prefix: prefix, Purpose: "token",
			ExpiresAt: &exp, Description: "omnibot run", UserAgent: ua, ClientIP: ip,
			Agent: "omnibot", AgentChannels: []string{"omnibot"},
		})
		if err != nil || !issued {
			release()
			return nil, huma.Error500InternalServerError("start the run")
		}
		revoke := func() {
			if err := gw.RevokeBearer(context.WithoutCancel(ctx), hash); err != nil {
				o.log.Error("omnibot run: revoke the run's token", "err", err)
			}
			release()
		}
		l := &assistant.Loop{Provider: model, Tools: mcpserver.NewLocal(o.self, "Bearer "+tok), Pause: true}

		waiting, open, err := l.Waiting(ctx, history)
		if err != nil {
			revoke()
			return nil, huma.Error500InternalServerError("read the waiting call")
		}
		var approved bool
		if len(in.Body.Resume) > 0 {
			r := in.Body.Resume[0]
			if !open || r.InterruptID != waiting.CallID {
				revoke()
				return nil, huma.Error409Conflict("no call by that id waits in this conversation; read it again")
			}
			approved = r.Status == "resolved" && approvedIn(r.Payload)
		} else if open {
			revoke()
			return nil, huma.Error409Conflict("a call waits in this conversation; answer it with a resume first")
		}

		runID := in.Body.RunID
		if runID == "" {
			runID = claim.Run
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			defer revoke()
			hctx.SetHeader("Content-Type", "text/event-stream")
			hctx.SetHeader("Cache-Control", "no-cache")
			hctx.SetStatus(http.StatusOK)
			rctx, cancel := context.WithDeadline(hctx.Context(), exp)
			defer cancel()
			run := agui.NewRun(hctx.BodyWriter(), conv.Name, runID)
			l.Events = run.Hear
			run.Start()

			var out assistant.Outcome
			var err error
			if len(in.Body.Resume) > 0 {
				out, err = l.Continue(rctx, history, approved)
			} else {
				l.Resume(history)
				out, err = l.Run(rctx, assistant.HandedOver(prompt, handover(in.Body.Context)))
			}
			if err != nil {
				o.log.Error("omnibot run", "conversation", conv.Name, "err", err)
				run.Error("the run stopped: "+err.Error(), "run")
			}
			if len(out.Messages) > len(history) {
				if err := keepRun(rctx, gw, conv.Name, claim.Run, ms, out); err != nil {
					o.log.Error("omnibot run: keep the transcript", "conversation", conv.Name, "err", err)
					run.Error("the run could not be kept", "store")
					return
				}
			}
			if out.Pending != nil {
				run.Interrupt(*out.Pending)
				return
			}
			if out.Stopped {
				run.Stopped(out.Steps)
			}
			run.Finish()
		}}, nil
	})
}

// runProvider is the model a run talks to, chosen as the run starts so a
// settings change is the next run's (#2109): the configured provider, its key
// resolved in process and its calls judged by the egress contract; else the
// server's recording; else none. A configured provider whose key is broken
// refuses the run rather than quietly playing the recording.
func runProvider(ctx context.Context, gw storage.Gateway, o options) (provider.Provider, error) {
	cfg, err := gw.OmnibotModel(ctx)
	if errors.Is(err, storage.ErrModelKeyInvalid) {
		return nil, huma.Error503ServiceUnavailable("omnibot's model: " + strings.TrimPrefix(err.Error(), storage.ErrModelKeyInvalid.Error()+": "))
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("read omnibot's model")
	}
	if cfg == nil {
		if o.omnibotModel == nil {
			return nil, huma.Error503ServiceUnavailable("omnibot has no model on this server")
		}
		return o.omnibotModel, nil
	}
	pol, err := gw.EgressPolicy(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("read the egress policy")
	}
	eo := o.egressOpts
	if eo.ResponseTimeout == 0 {
		eo.ResponseTimeout = modelTimeout
	}
	return &provider.OpenAI{BaseURL: cfg.BaseURL, Model: cfg.Model, Key: cfg.Key, System: provider.DefaultSystemPrompt,
		Client: egress.New(pol, eo)}, nil
}

// replayUnmatchedType names the refusal of a question the server's recording
// has no script for, so a client tells it from any other 422.
const replayUnmatchedType = "urn:omniglass:omnibot-replay-unmatched"

func replayUnmatched() error {
	err := huma.Error422UnprocessableEntity("This lab plays a recording; connect a model to ask your own question")
	var em *huma.ErrorModel
	if errors.As(err, &em) {
		em.Type = replayUnmatchedType
		em.Title = "Unrecorded question"
	}
	return err
}

// omnibotFacts is what omnibot answers from, read as a client asks: a
// configured model is live whatever its key's state, else a server's
// recording is a replay of the prompts it answers, else omnibot has no model.
// Nothing of the configuration is told.
func omnibotFacts(ctx context.Context, gw storage.Gateway, o options) (assistantOmnibotFacts, error) {
	facts := assistantOmnibotFacts{Mode: "none", Prompts: []string{}}
	cfg, err := gw.OmnibotModel(ctx)
	if err != nil && !errors.Is(err, storage.ErrModelKeyInvalid) {
		return facts, huma.Error500InternalServerError("read omnibot's model")
	}
	switch {
	case err != nil || cfg != nil:
		facts.Mode = "live"
	case o.omnibotModel != nil:
		facts.Mode = "replay"
		if sc, ok := o.omnibotModel.(provider.Scripted); ok {
			facts.Prompts = append(facts.Prompts, sc.Prompts()...)
		}
	}
	return facts, nil
}

// runHistory is the stored transcript as the loop carries it on.
func runHistory(ms []storage.AssistantMessage) ([]provider.Message, error) {
	out := make([]provider.Message, 0, len(ms))
	for _, m := range ms {
		var pm provider.Message
		if err := json.Unmarshal(m.Body, &pm); err != nil {
			return nil, err
		}
		out = append(out, pm)
	}
	return out, nil
}

// keepRun appends what the run added after the stored transcript, and keeps
// or clears the call it paused on, under the run's claim.
func keepRun(ctx context.Context, gw storage.Gateway, name, run string, ms []storage.AssistantMessage, out assistant.Outcome) error {
	app := storage.AssistantAppend{Run: run}
	if len(ms) > 0 {
		app.After = ms[len(ms)-1].Seq
	}
	for _, m := range out.Messages[len(ms):] {
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		app.Messages = append(app.Messages, storage.AssistantMessage{Role: string(m.Role), Body: b})
	}
	if out.Pending != nil {
		b, err := json.Marshal(out.Pending)
		if err != nil {
			return err
		}
		app.Pending = b
	}
	_, err := gw.AppendAssistantMessages(context.WithoutCancel(ctx), actorID(ctx), callerID(ctx), name, app)
	return err
}

// lastUserText is the text of the last user message: a string, or the text
// parts of an array of parts.
func lastUserText(msgs []aguiMessageInput) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		var s string
		if json.Unmarshal(msgs[i].Content, &s) == nil {
			return strings.TrimSpace(s)
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(msgs[i].Content, &parts) != nil {
			return ""
		}
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				b.WriteString(p.Text)
			}
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

// skillPrompt is the prompt a skill run sends: the embedded skill's own, with
// its arguments, then the person's message when they wrote one.
func skillPrompt(in aguiSkillInput, message string) (string, error) {
	s, ok := skills.Get(in.Name)
	if !ok {
		return "", fmt.Errorf("no skill is named %q", in.Name)
	}
	p, err := s.Prompt(in.Arguments)
	if err != nil {
		return "", err
	}
	if message != "" {
		p = strings.TrimRight(p, "\n") + "\n\nTheir message:\n" + message
	}
	return p, nil
}

func handover(in []aguiContextInput) []assistant.Handover {
	out := make([]assistant.Handover, len(in))
	for i, c := range in {
		out[i] = assistant.Handover{Description: c.Description, Value: c.Value}
	}
	return out
}

func approvedIn(payload json.RawMessage) bool {
	var p struct {
		Approved bool `json:"approved"`
	}
	return json.Unmarshal(payload, &p) == nil && p.Approved
}
