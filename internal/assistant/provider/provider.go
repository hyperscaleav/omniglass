// Package provider is omnibot's model seam (#1166): the conversation the loop
// keeps, the tools it offers, and the one call a model answers, so the loop
// runs the same against a live model and a recorded one. Replay is the
// recorded one: no network and no key, for tests, demos and CI.
package provider

import "context"

// Role is who said a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one entry in the conversation: the person's prompt, a model
// turn (its text and the calls it asked for), or a tool's result answering
// the call named by ToolCallID.
type Message struct {
	Role       Role
	Text       string
	ToolCalls  []ToolCall
	ToolCallID string
	IsError    bool
}

// Tool is a tool offered to the model: its name, what it does, and the JSON
// Schema of its arguments.
type Tool struct {
	Name        string
	Description string
	InputSchema any
}

// ToolCall is a call the model asked for.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// Turn is a model's answer: text for the person, calls to make, or both. A
// turn with no calls ends the run.
type Turn struct {
	Text      string
	ToolCalls []ToolCall
}

// Provider answers the next turn of a conversation, given the tools offered.
type Provider interface {
	Next(ctx context.Context, messages []Message, tools []Tool) (Turn, error)
}
