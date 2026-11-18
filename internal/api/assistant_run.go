package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/assistant"
	"github.com/hyperscaleav/omniglass/internal/assistant/agui"
	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
	"github.com/hyperscaleav/omniglass/internal/auth"
	mcpserver "github.com/hyperscaleav/omniglass/internal/mcp"
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

type runAssistantConversationInput struct {
	Name string `path:"name" doc:"Your conversation's name"`
	Body struct {
		_        struct{}           `additionalProperties:"true"`
		ThreadID string             `json:"threadId,omitempty" required:"false" doc:"Ignored: the conversation is the thread"`
		RunID    string             `json:"runId,omitempty" required:"false" doc:"The run's id, echoed in its events"`
		Messages []aguiMessageInput `json:"messages,omitempty" required:"false" doc:"The client's view of the conversation; only its last user message is read, as the prompt"`
		Resume   []aguiResumeInput  `json:"resume,omitempty" required:"false" doc:"The answer to the interrupt the last run ended on"`
		Context  []aguiContextInput `json:"context,omitempty" required:"false" maxItems:"20" doc:"What the person handed over with the prompt, read as data ahead of it; ignored on a resume"`
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
				"A server with no model is a 503. Gated by assistant:use.",
	}, "assistant", "use"), func(ctx context.Context, in *runAssistantConversationInput) (*huma.StreamResponse, error) {
		if o.omnibotModel == nil {
			return nil, huma.Error503ServiceUnavailable("omnibot has no model on this server")
		}
		u, ok := userFrom(ctx)
		if !ok || u.Person == nil || u.Agent != nil || impersonationMode(ctx) != "" {
			return nil, huma.Error403Forbidden("only a person, as themselves, runs omnibot")
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
		if len(in.Body.Resume) == 0 && prompt == "" {
			return nil, huma.Error422UnprocessableEntity("a run needs a user message or a resume")
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
		l := &assistant.Loop{Provider: o.omnibotModel, Tools: mcpserver.NewLocal(o.self, "Bearer "+tok), Pause: true}

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
