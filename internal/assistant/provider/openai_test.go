package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testKey = "ghp-test-key-never-shown"

// chatServer answers chat completions with reply, keeping the last request
// it was sent.
func chatServer(t *testing.T, status int, reply string) (*httptest.Server, *map[string]any, *http.Header) {
	t.Helper()
	var got map[string]any
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		hdr = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("the request is not JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, &got, &hdr
}

// The adapter sends the conversation, the system prompt and the tools in the
// chat completions shape, authenticated by the key, and reads a turn of calls.
func TestOpenAISendsConversationAndReadsCalls(t *testing.T) {
	t.Parallel()
	srv, got, hdr := chatServer(t, 200, `{"choices":[{"message":{"role":"assistant","content":"Looking.",
		"tool_calls":[
			{"id":"c1","type":"function","function":{"name":"get-component","arguments":"{\"name\":\"lobby-display\"}"}},
			{"id":"c2","type":"function","function":{"name":"list-alarms","arguments":""}}]}}]}`)
	p := &OpenAI{BaseURL: srv.URL + "/", Model: "openai/gpt-4.1-mini", Key: testKey, System: "You are omnibot."}
	turn, err := p.Next(context.Background(), []Message{
		{Role: RoleUser, Text: "tag the lobby display"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c0", Name: "list-components", Arguments: map[string]any{"q": "lobby"}}}},
		{Role: RoleTool, ToolCallID: "c0", Text: `{"items":[]}`},
	}, []Tool{{Name: "get-component", Description: "Read a component", InputSchema: map[string]any{"type": "object"}}})
	if err != nil {
		t.Fatal(err)
	}
	if turn.Text != "Looking." || len(turn.ToolCalls) != 2 {
		t.Fatalf("turn = %+v", turn)
	}
	if c := turn.ToolCalls[0]; c.ID != "c1" || c.Name != "get-component" || c.Arguments["name"] != "lobby-display" {
		t.Errorf("first call = %+v", c)
	}
	if c := turn.ToolCalls[1]; c.ID != "c2" || c.Name != "list-alarms" || c.Arguments != nil {
		t.Errorf("a call with no arguments = %+v", c)
	}
	if a := hdr.Get("Authorization"); a != "Bearer "+testKey {
		t.Errorf("Authorization = %q", a)
	}
	body := *got
	if body["model"] != "openai/gpt-4.1-mini" {
		t.Errorf("model = %v", body["model"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages = %v", msgs)
	}
	roles := []string{}
	for _, m := range msgs {
		roles = append(roles, m.(map[string]any)["role"].(string))
	}
	if strings.Join(roles, ",") != "system,user,assistant,tool" {
		t.Errorf("roles = %v", roles)
	}
	asst := msgs[2].(map[string]any)
	calls := asst["tool_calls"].([]any)
	fn := calls[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "list-components" || fn["arguments"] != `{"q":"lobby"}` {
		t.Errorf("assistant call = %v", fn)
	}
	if tool := msgs[3].(map[string]any); tool["tool_call_id"] != "c0" || tool["content"] != `{"items":[]}` {
		t.Errorf("tool message = %v", tool)
	}
	tools := body["tools"].([]any)
	tf := tools[0].(map[string]any)["function"].(map[string]any)
	if tf["name"] != "get-component" || tf["description"] != "Read a component" || tf["parameters"] == nil {
		t.Errorf("tool = %v", tf)
	}
}

// A turn of text alone ends the run, and a request with no tools sends none.
func TestOpenAIReadsText(t *testing.T) {
	t.Parallel()
	srv, got, _ := chatServer(t, 200, `{"choices":[{"message":{"role":"assistant","content":"Done."}}]}`)
	p := &OpenAI{BaseURL: srv.URL, Model: "m", Key: testKey}
	turn, err := p.Next(context.Background(), []Message{{Role: RoleUser, Text: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Text != "Done." || len(turn.ToolCalls) != 0 {
		t.Errorf("turn = %+v", turn)
	}
	if _, ok := (*got)["tools"]; ok {
		t.Errorf("tools sent with none offered: %v", (*got)["tools"])
	}
}

// Each failure names its cause and never carries the key, even when the
// provider echoes it back.
func TestOpenAIErrorsNameTheCauseNotTheKey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{401, `{"error":{"message":"Bad credentials ` + testKey + `"}}`, "refused the key"},
		// A long message that echoes the key across the cut must not keep its head.
		{401, `{"error":{"message":"` + strings.Repeat("x", 190) + testKey + `"}}`, "refused the key"},
		{429, `{"error":{"message":"rate limited"}}`, "rate limit"},
		{503, `upstream down ` + testKey, "503"},
		{200, `{"choices":[]}`, "no answer"},
		{200, `not json`, "could not be read"},
		{200, `{"choices":[{"message":{"tool_calls":[{"id":"c","function":{"name":"x","arguments":"{bad"}}]}}]}`, "arguments"},
	} {
		srv, _, _ := chatServer(t, tc.status, tc.body)
		p := &OpenAI{BaseURL: srv.URL, Model: "m", Key: testKey}
		_, err := p.Next(context.Background(), []Message{{Role: RoleUser, Text: "hi"}}, nil)
		if err == nil {
			t.Errorf("%d %s: no error", tc.status, tc.body)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%d: %q does not say %q", tc.status, err, tc.want)
		}
		if strings.Contains(err.Error(), testKey[:8]) {
			t.Errorf("%d: the error carries the key: %q", tc.status, err)
		}
	}
}

// An unreachable server is an error naming the host, not the key.
func TestOpenAIUnreachable(t *testing.T) {
	t.Parallel()
	p := &OpenAI{BaseURL: "http://127.0.0.1:1", Model: "m", Key: testKey}
	_, err := p.Next(context.Background(), []Message{{Role: RoleUser, Text: "hi"}}, nil)
	if err == nil || strings.Contains(err.Error(), testKey) || !strings.Contains(err.Error(), "reach") {
		t.Errorf("err = %v", err)
	}
}

// Bedrock's gpt-oss inlines its chain of thought in content; the adapter keeps
// it apart from the answer (#2259).
func TestOpenAISplitsInlineReasoning(t *testing.T) {
	t.Parallel()
	srv, got, _ := chatServer(t, 200, `{"choices":[{"message":{"role":"assistant","content":"<reasoning>User asks 2+2. Simple.</reasoning>The sum is **4**."}}]}`)
	p := &OpenAI{BaseURL: srv.URL, Model: "m", Key: testKey}
	turn, err := p.Next(context.Background(), []Message{{Role: RoleUser, Text: "2+2?"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Text != "The sum is **4**." || turn.Reasoning != "User asks 2+2. Simple." {
		t.Fatalf("turn = %+v", turn)
	}
	// A follow-up built from that turn never carries the reasoning back.
	_, err = p.Next(context.Background(), []Message{
		{Role: RoleUser, Text: "2+2?"},
		{Role: RoleAssistant, Text: turn.Text, Reasoning: turn.Reasoning},
		{Role: RoleUser, Text: "and 3+3?"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(*got)
	if strings.Contains(string(b), "Simple.") || strings.Contains(string(b), "reasoning") {
		t.Errorf("the request carries the reasoning: %s", b)
	}
}

// Providers that send reasoning in its own field have it kept, not dropped.
func TestOpenAIReadsReasoningFields(t *testing.T) {
	t.Parallel()
	for field, reply := range map[string]string{
		"reasoning_content": `{"choices":[{"message":{"role":"assistant","content":"Four.","reasoning_content":"add them"}}]}`,
		"reasoning":         `{"choices":[{"message":{"role":"assistant","content":"Four.","reasoning":"add them"}}]}`,
	} {
		srv, _, _ := chatServer(t, 200, reply)
		turn, err := (&OpenAI{BaseURL: srv.URL, Model: "m", Key: testKey}).Next(context.Background(), []Message{{Role: RoleUser, Text: "x"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if turn.Text != "Four." || turn.Reasoning != "add them" {
			t.Errorf("%s: turn = %+v", field, turn)
		}
	}
}

func TestSplitReasoning(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ in, reasoning, answer string }{
		{"plain", "", "plain"},
		{"<reasoning>a</reasoning>b", "a", "b"},
		{"  <reasoning>a</reasoning>\n<reasoning>c</reasoning> b ", "a\n\nc", "b"},
		{"<reasoning>never closed", "never closed", ""},
		{"answer <reasoning>not leading</reasoning>", "", "answer <reasoning>not leading</reasoning>"},
	} {
		r, a := splitReasoning(c.in)
		if r != c.reasoning || a != c.answer {
			t.Errorf("splitReasoning(%q) = %q, %q; want %q, %q", c.in, r, a, c.reasoning, c.answer)
		}
	}
}
