package provider

import (
	"context"
	"os"
	"testing"
	"time"
)

// GitHub Models answers a real tool-calling turn (#2109). It runs only with
// OMNIGLASS_TEST_GITHUB_MODELS_TOKEN set (a GitHub token with models:read,
// such as gh auth token), and is run before a merge that touches the adapter.
func TestOpenAILiveGitHubModels(t *testing.T) {
	key := os.Getenv("OMNIGLASS_TEST_GITHUB_MODELS_TOKEN")
	if key == "" {
		t.Skip("OMNIGLASS_TEST_GITHUB_MODELS_TOKEN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p := &OpenAI{BaseURL: DefaultOpenAIBaseURL, Model: "openai/gpt-4.1-mini", Key: key, System: DefaultSystemPrompt}
	tools := []Tool{{
		Name:        "get-component",
		Description: "Read one component of the fleet by name",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
			"required":   []string{"name"},
		},
	}}
	msgs := []Message{{Role: RoleUser, Text: "What is the state of the component named lobby-display? Use the tool."}}
	turn, err := p.Next(ctx, msgs, tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Name != "get-component" || turn.ToolCalls[0].Arguments["name"] != "lobby-display" {
		t.Fatalf("the model did not call get-component for lobby-display: %+v", turn)
	}
	msgs = append(msgs,
		Message{Role: RoleAssistant, Text: turn.Text, ToolCalls: turn.ToolCalls},
		Message{Role: RoleTool, ToolCallID: turn.ToolCalls[0].ID, Text: `{"name":"lobby-display","status":"healthy"}`})
	final, err := p.Next(ctx, msgs, tools)
	if err != nil {
		t.Fatal(err)
	}
	if final.Text == "" {
		t.Fatalf("no answer after the tool result: %+v", final)
	}
	t.Logf("model: %s", final.Text)
}
