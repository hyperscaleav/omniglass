package provider

import (
	"context"
	"os"
	"testing"
	"time"
)

// A real OpenAI-compatible server answers a tool-calling turn (#2109). It runs
// only with OMNIGLASS_TEST_MODEL_KEY set, and is run before a merge that
// touches the adapter. OMNIGLASS_TEST_MODEL_BASE_URL and
// OMNIGLASS_TEST_MODEL_NAME pick the server and model, the install's
// defaults when unset, so any provider the adapter claims can be proven:
// Bedrock's gpt-oss is base URL
// https://bedrock-runtime.us-west-2.amazonaws.com/openai/v1 and model
// openai.gpt-oss-20b-1:0, with a Bedrock API key.
func TestOpenAILive(t *testing.T) {
	key := os.Getenv("OMNIGLASS_TEST_MODEL_KEY")
	if key == "" {
		t.Skip("OMNIGLASS_TEST_MODEL_KEY is not set")
	}
	baseURL, model := os.Getenv("OMNIGLASS_TEST_MODEL_BASE_URL"), os.Getenv("OMNIGLASS_TEST_MODEL_NAME")
	if baseURL == "" {
		baseURL = DefaultOpenAIBaseURL
	}
	if model == "" {
		model = "openai/gpt-4.1-mini"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p := &OpenAI{BaseURL: baseURL, Model: model, Key: key, System: DefaultSystemPrompt}
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
