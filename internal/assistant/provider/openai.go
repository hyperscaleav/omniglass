package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultOpenAIBaseURL is OpenRouter, the install's model when it names
// no other OpenAI-compatible server (#2109).
const DefaultOpenAIBaseURL = "https://openrouter.ai/api/v1"

// DefaultSystemPrompt frames every live turn: who omnibot is and how it
// reaches the fleet.
const DefaultSystemPrompt = "You are omnibot, the assistant inside Omniglass, an observability and control plane for AV and IT fleets. " +
	"You act for the person you talk to, with their permissions, only through the tools offered. " +
	"Read before you change, address things by name, and say plainly what you did and what you found. " +
	"A credential is never yours to see: a tool tells you only whether one is present."

// OpenAI is a live model behind any OpenAI-compatible chat completions
// server: OpenRouter by default, or OpenAI, Ollama, vLLM and LM Studio by
// base URL. Key is the bearer the server is called with; it is never part of
// an error.
type OpenAI struct {
	BaseURL string
	Model   string
	Key     string
	System  string
	// Client sends the request; the server passes its egress client, so the
	// call is judged by the install's egress contract. Nil is a plain client.
	Client Doer
}

// Doer sends one HTTP request: an *http.Client or an *egress.Client.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type chatMessage struct {
	Role       string     `json:"role"`
	Content    *string    `json:"content"`
	ToolCalls  []chatCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	// Reasoning is read only: the servers that send a chain of thought in a
	// field of its own name it one of these. It is never written back.
	ReasoningContent string `json:"reasoning_content,omitempty"`
	Reasoning        string `json:"reasoning,omitempty"`
}

type chatCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Parameters  any    `json:"parameters,omitempty"`
	} `json:"function"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// Next asks the server for the next turn.
func (p *OpenAI) Next(ctx context.Context, messages []Message, tools []Tool) (Turn, error) {
	body, err := json.Marshal(p.request(messages, tools))
	if err != nil {
		return Turn{}, fmt.Errorf("the model request: %w", err)
	}
	endpoint := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Turn{}, fmt.Errorf("the model's base URL: %w", p.scrub(err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.Key)
	var client Doer = &http.Client{Timeout: 2 * time.Minute}
	if p.Client != nil {
		client = p.Client
	}
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return Turn{}, fmt.Errorf("could not reach the model at %s: %w", req.URL.Host, p.scrub(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Turn{}, fmt.Errorf("read the model's answer: %w", p.scrub(err))
	}
	if resp.StatusCode != http.StatusOK {
		return Turn{}, p.statusErr(resp.StatusCode, raw)
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Turn{}, errors.New("the model's answer could not be read")
	}
	if len(out.Choices) == 0 {
		return Turn{}, errors.New("the model gave no answer")
	}
	return turnFrom(out.Choices[0].Message)
}

func (p *OpenAI) request(messages []Message, tools []Tool) chatRequest {
	r := chatRequest{Model: p.Model}
	if p.System != "" {
		r.Messages = append(r.Messages, chatMessage{Role: "system", Content: text(p.System)})
	}
	for _, m := range messages {
		cm := chatMessage{Role: string(m.Role), ToolCallID: m.ToolCallID}
		if m.Text != "" || m.Role != RoleAssistant {
			cm.Content = text(m.Text)
		}
		for _, c := range m.ToolCalls {
			var wc chatCall
			wc.ID, wc.Type = c.ID, "function"
			wc.Function.Name = c.Name
			wc.Function.Arguments = "{}"
			if len(c.Arguments) > 0 {
				if b, err := json.Marshal(c.Arguments); err == nil {
					wc.Function.Arguments = string(b)
				}
			}
			cm.ToolCalls = append(cm.ToolCalls, wc)
		}
		r.Messages = append(r.Messages, cm)
	}
	for _, t := range tools {
		var ct chatTool
		ct.Type = "function"
		ct.Function.Name, ct.Function.Description, ct.Function.Parameters = t.Name, t.Description, t.InputSchema
		r.Tools = append(r.Tools, ct)
	}
	return r
}

func turnFrom(m chatMessage) (Turn, error) {
	var t Turn
	if m.Content != nil {
		t.Reasoning, t.Text = splitReasoning(*m.Content)
	}
	field := strings.TrimSpace(m.ReasoningContent)
	if field == "" {
		field = strings.TrimSpace(m.Reasoning)
	}
	if field != "" {
		if t.Reasoning != "" {
			field += "\n\n" + t.Reasoning
		}
		t.Reasoning = field
	}
	for _, c := range m.ToolCalls {
		call := ToolCall{ID: c.ID, Name: c.Function.Name}
		if a := strings.TrimSpace(c.Function.Arguments); a != "" && a != "{}" {
			if err := json.Unmarshal([]byte(a), &call.Arguments); err != nil {
				return Turn{}, fmt.Errorf("the model called %s with arguments that are not a JSON object", c.Function.Name)
			}
		}
		t.ToolCalls = append(t.ToolCalls, call)
	}
	if t.Text == "" && len(t.ToolCalls) == 0 {
		return Turn{}, errors.New("the model gave no answer")
	}
	return t, nil
}

const (
	reasoningOpen  = "<reasoning>"
	reasoningClose = "</reasoning>"
)

// splitReasoning pulls the leading <reasoning> blocks (Bedrock's gpt-oss
// inlines its chain of thought so) off a message's content: the blocks
// joined, and the answer after them. An unclosed block is all reasoning.
func splitReasoning(content string) (reasoning, answer string) {
	rest := strings.TrimSpace(content)
	var blocks []string
	for strings.HasPrefix(rest, reasoningOpen) {
		body := rest[len(reasoningOpen):]
		end := strings.Index(body, reasoningClose)
		if end < 0 {
			blocks = append(blocks, strings.TrimSpace(body))
			rest = ""
			break
		}
		blocks = append(blocks, strings.TrimSpace(body[:end]))
		rest = strings.TrimSpace(body[end+len(reasoningClose):])
	}
	return strings.Join(blocks, "\n\n"), rest
}

// statusErr names a refused request by its cause, with the server's own
// message when it gives one, the key cut out of it.
func (p *OpenAI) statusErr(status int, raw []byte) error {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	detail := ""
	if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
		// Scrub before cutting: a cut through an echoed key would leave its
		// head where ReplaceAll can no longer see it.
		detail = p.scrubText(e.Error.Message)
		if len(detail) > 200 {
			detail = strings.ToValidUTF8(detail[:200], "")
		}
		detail = ": " + detail
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fmt.Errorf("the model server refused the key (%d)%s", status, detail)
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("the model server's rate limit was reached (429)%s", detail)
	default:
		return fmt.Errorf("the model server answered %d%s", status, detail)
	}
}

func (p *OpenAI) scrub(err error) error { return errors.New(p.scrubText(err.Error())) }

func (p *OpenAI) scrubText(s string) string {
	if p.Key == "" {
		return s
	}
	return strings.ReplaceAll(s, p.Key, "[key]")
}

func text(s string) *string { return &s }
