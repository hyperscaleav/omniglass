package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Stored omnibot conversations (#2113): a person's named conversations with
// omnibot, kept so a client can carry one on later. Every route is gated by
// assistant:use and is owner-only: the ownership predicate rides every query,
// so anyone else's conversation, the platform owner's view included, is a
// non-disclosing 404. A paused run's pending write is kept as the transcript's
// last word; nothing on the server acts on it (ADR-2113.2).

type assistantMessageBody struct {
	Seq       int             `json:"seq" doc:"The message's position in the conversation, from 1"`
	Role      string          `json:"role" enum:"user,assistant,tool" doc:"Who said it"`
	Body      json.RawMessage `json:"body" doc:"The message as the client's loop carries it on, kept as given"`
	CreatedAt time.Time       `json:"created_at"`
}

type assistantConversationOutput struct {
	Body struct {
		storage.AssistantConversation
		Transcript []assistantMessageBody `json:"messages" doc:"The transcript, in order"`
	}
}

type assistantConversationSummaryOutput struct {
	Body storage.AssistantConversation
}

type listAssistantConversationsOutput struct {
	Body struct {
		Conversations []storage.AssistantConversation `json:"conversations"`
	}
}

type assistantConversationRefInput struct {
	Name string `path:"name" doc:"Your conversation's name"`
}

type createAssistantConversationInput struct {
	Body *struct {
		Name string `json:"name,omitempty" required:"false" doc:"A name (lowercase kebab), unique among your own conversations; absent names it for you"`
	} `required:"false"`
}

type appendAssistantMessageInput struct {
	Role string          `json:"role" enum:"user,assistant,tool" doc:"Who said it"`
	Body json.RawMessage `json:"body" doc:"The message as your loop carries it on"`
}

type appendAssistantConversationInput struct {
	Name string `path:"name" doc:"Your conversation's name"`
	Body struct {
		After    int                           `json:"after" minimum:"0" doc:"The seq of the last message you saw (0 for an empty conversation); anything else is a 409"`
		Messages []appendAssistantMessageInput `json:"messages" minItems:"1" doc:"The new messages, in order"`
		Pending  json.RawMessage               `json:"pending,omitempty" required:"false" doc:"The write the run paused on, waiting on you; absent clears it"`
		Usage    *storage.AssistantUsage       `json:"usage,omitempty" required:"false" doc:"The tokens this turn spent, added to the total"`
	}
}

func registerAssistantRoutes(api huma.API, a *authenticator, gw storage.Gateway, o options) {
	registerAssistantRunRoute(api, a, gw, o)
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-assistant-conversations",
		Method:      http.MethodGet,
		Path:        "/assistant/conversations",
		Summary:     "List your omnibot conversations",
		Description: "Your own stored conversations, most recently moved first, without their messages. Owner-only. Gated by assistant:use.",
	}, "assistant", "use"), func(ctx context.Context, _ *struct{}) (*listAssistantConversationsOutput, error) {
		cs, err := gw.ListAssistantConversations(ctx, callerID(ctx))
		if err != nil {
			return nil, mapAssistantErr(err)
		}
		out := &listAssistantConversationsOutput{}
		out.Body.Conversations = cs
		if out.Body.Conversations == nil {
			out.Body.Conversations = []storage.AssistantConversation{}
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-assistant-conversation",
		Method:      http.MethodGet,
		Path:        "/assistant/conversations/{name}",
		Summary:     "Get one of your omnibot conversations",
		Description: "One of your own conversations with its transcript, its pending write if a run paused on one, and the tokens it has spent. Anyone else's is a 404. Gated by assistant:use.",
	}, "assistant", "use"), func(ctx context.Context, in *assistantConversationRefInput) (*assistantConversationOutput, error) {
		c, ms, err := gw.GetAssistantConversation(ctx, callerID(ctx), in.Name)
		if err != nil {
			return nil, mapAssistantErr(err)
		}
		out := &assistantConversationOutput{}
		out.Body.AssistantConversation = *c
		out.Body.Transcript = make([]assistantMessageBody, 0, len(ms))
		for _, m := range ms {
			out.Body.Transcript = append(out.Body.Transcript, assistantMessageBody{Seq: m.Seq, Role: m.Role, Body: m.Body, CreatedAt: m.CreatedAt})
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-assistant-conversation",
		Method:        http.MethodPost,
		Path:          "/assistant/conversations",
		DefaultStatus: http.StatusCreated,
		Summary:       "Start an omnibot conversation",
		Description:   "Creates an empty conversation of your own, named or named for you. A name you already use is a 409. Audited. Gated by assistant:use.",
	}, "assistant", "use"), func(ctx context.Context, in *createAssistantConversationInput) (*assistantConversationSummaryOutput, error) {
		name := ""
		if in.Body != nil {
			name = in.Body.Name
		}
		c, err := gw.CreateAssistantConversation(ctx, actorID(ctx), callerID(ctx), name)
		if err != nil {
			return nil, mapAssistantErr(err)
		}
		return &assistantConversationSummaryOutput{Body: *c}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "append-assistant-conversation",
		Method:      http.MethodPost,
		Path:        "/assistant/conversations/{name}:append",
		Summary:     "Append a turn to one of your omnibot conversations",
		Description: "Adds a turn's messages after the one you last saw (after), keeps or clears the write a run paused on (pending), and adds the turn's tokens. An after that is not the conversation's last message is a 409: another client wrote first, so read it again. Anyone else's is a 404. Audited without content. Gated by assistant:use.",
	}, "assistant", "use"), func(ctx context.Context, in *appendAssistantConversationInput) (*assistantConversationSummaryOutput, error) {
		app := storage.AssistantAppend{After: in.Body.After, Pending: in.Body.Pending}
		if string(app.Pending) == "null" {
			app.Pending = nil
		}
		if in.Body.Usage != nil {
			app.Usage = *in.Body.Usage
		}
		for _, m := range in.Body.Messages {
			app.Messages = append(app.Messages, storage.AssistantMessage{Role: m.Role, Body: m.Body})
		}
		c, err := gw.AppendAssistantMessages(ctx, actorID(ctx), callerID(ctx), in.Name, app)
		if err != nil {
			return nil, mapAssistantErr(err)
		}
		return &assistantConversationSummaryOutput{Body: *c}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-assistant-conversation",
		Method:        http.MethodDelete,
		Path:          "/assistant/conversations/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete one of your omnibot conversations",
		Description:   "Deletes one of your own conversations and its transcript. Anyone else's is a 404. Audited. Gated by assistant:use.",
	}, "assistant", "use"), func(ctx context.Context, in *assistantConversationRefInput) (*struct{}, error) {
		if err := gw.DeleteAssistantConversation(ctx, actorID(ctx), callerID(ctx), in.Name); err != nil {
			return nil, mapAssistantErr(err)
		}
		return nil, nil
	})
}

func mapAssistantErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrAssistantConversationNotFound):
		return huma.Error404NotFound("no such conversation of yours")
	case errors.Is(err, storage.ErrAssistantConversationExists):
		return huma.Error409Conflict("you already have a conversation by that name")
	case errors.Is(err, storage.ErrAssistantConversationMoved):
		return huma.Error409Conflict("the conversation has moved on since you read it; read it again and append after its last message")
	case errors.Is(err, storage.ErrInvalidEntityName):
		return huma.Error422UnprocessableEntity(err.Error())
	}
	return err
}
