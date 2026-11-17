package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
)

// catalog stands in for the MCP server's tools: each answers with its own
// name, and the calls it received are kept. The loop is under test, not it.
type catalog struct {
	tools  []*sdk.Tool
	called []string
	fail   error
}

func tool(name, policy string) *sdk.Tool {
	t := &sdk.Tool{Name: name, Title: "Do " + name, InputSchema: map[string]any{"type": "object"}}
	if policy != "" {
		t.Meta = sdk.Meta{"omniglass/agent_policy": map[string]any{"class": "write", "policy": policy}}
	}
	return t
}

func (c *catalog) List(context.Context) ([]*sdk.Tool, error) { return c.tools, nil }

func (c *catalog) Call(_ context.Context, name string, _ map[string]any) (*sdk.CallToolResult, error) {
	if c.fail != nil {
		return nil, c.fail
	}
	c.called = append(c.called, name)
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: name + " done"}}}, nil
}

// offered records the tool names each model turn was offered.
type offered struct {
	provider.Provider
	names [][]string
	last  []provider.Message
}

func (o *offered) Next(ctx context.Context, msgs []provider.Message, tools []provider.Tool) (provider.Turn, error) {
	var names []string
	for _, t := range tools {
		names = append(names, t.Name)
	}
	o.names = append(o.names, names)
	o.last = msgs
	return o.Provider.Next(ctx, msgs, tools)
}

func replay(t *testing.T, doc string) *offered {
	t.Helper()
	r, err := provider.ParseReplay([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return &offered{Provider: r}
}

func confirmer(answer bool, asked *[]Proposal) Confirmer {
	return func(_ context.Context, p Proposal) (bool, error) {
		*asked = append(*asked, p)
		return answer, nil
	}
}

func fleet() *catalog {
	return &catalog{tools: []*sdk.Tool{
		tool("list-components", "always"),
		tool("create-component", "ask"),
		tool("delete-component", "never"),
		tool("unmarked", ""),
	}}
}

// An always tool runs without asking; an ask tool runs only once the person
// confirms it; the run ends on the model's text.
func TestLoopRunsAlwaysAndConfirmedAsk(t *testing.T) {
	t.Parallel()
	cat, p := fleet(), replay(t, `
turns:
  - tool_calls: [{name: list-components}, {name: create-component, arguments: {name: x}}]
  - text: Done.
`)
	var asked []Proposal
	out, err := (&Loop{Provider: p, Tools: cat, Confirm: confirmer(true, &asked)}).Run(context.Background(), "add x")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cat.called, ",") != "list-components,create-component" {
		t.Errorf("called = %v", cat.called)
	}
	if len(asked) != 1 || asked[0].Tool != "create-component" || asked[0].Title != "Do create-component" || asked[0].Arguments["name"] != "x" {
		t.Errorf("asked = %+v, want create-component alone", asked)
	}
	if out.Text != "Done." || out.Stopped || out.Steps != 2 {
		t.Errorf("outcome = %+v", out)
	}
	if got := decisions(out); got != "list-components:called,create-component:called" {
		t.Errorf("decisions = %s", got)
	}
}

// A declined ask calls nothing and tells the model it was declined, so the
// model can answer the person rather than retry blind.
func TestLoopDeclinedAskCallsNothing(t *testing.T) {
	t.Parallel()
	cat, p := fleet(), replay(t, `
turns:
  - tool_calls: [{name: create-component, arguments: {name: x}}]
  - text: Not created.
`)
	var asked []Proposal
	out, err := (&Loop{Provider: p, Tools: cat, Confirm: confirmer(false, &asked)}).Run(context.Background(), "add x")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.called) != 0 {
		t.Errorf("a declined call ran: %v", cat.called)
	}
	if got := decisions(out); got != "create-component:declined" {
		t.Errorf("decisions = %s", got)
	}
	res := p.last[len(p.last)-1]
	if res.Role != provider.RoleTool || res.ToolCallID != "call-1" || !res.IsError || !strings.Contains(res.Text, "declined") {
		t.Errorf("the model was told %+v, want the call declined", res)
	}
}

// With no confirmer (no one to ask), every ask is declined.
func TestLoopWithoutConfirmerDeclines(t *testing.T) {
	t.Parallel()
	cat, p := fleet(), replay(t, "turns:\n  - tool_calls: [{name: create-component}]\n  - text: ok\n")
	out, err := (&Loop{Provider: p, Tools: cat}).Run(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.called) != 0 || decisions(out) != "create-component:declined" {
		t.Errorf("called %v, decisions %s", cat.called, decisions(out))
	}
}

// A never tool is not offered; a call to it anyway, or to a tool the server
// does not have, is refused without asking. A tool with no policy is ask.
func TestLoopNeverIsNotOfferedAndUnmarkedAsks(t *testing.T) {
	t.Parallel()
	cat, p := fleet(), replay(t, `
turns:
  - tool_calls: [{name: delete-component}, {name: no-such-tool}, {name: unmarked}]
  - text: ok
`)
	var asked []Proposal
	out, err := (&Loop{Provider: p, Tools: cat, Confirm: confirmer(true, &asked)}).Run(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.names[0], ",") != "list-components,create-component,unmarked" {
		t.Errorf("offered = %v, want the never tool left out", p.names[0])
	}
	if strings.Join(cat.called, ",") != "unmarked" || len(asked) != 1 || asked[0].Tool != "unmarked" {
		t.Errorf("called %v, asked %+v", cat.called, asked)
	}
	if got := decisions(out); got != "delete-component:refused,no-such-tool:refused,unmarked:called" {
		t.Errorf("decisions = %s", got)
	}
}

// A model that never stops is stopped at the step ceiling, and the outcome
// says so.
func TestLoopStopsAtStepCeiling(t *testing.T) {
	t.Parallel()
	cat, p := fleet(), replay(t, "turns:\n"+strings.Repeat("  - tool_calls: [{name: list-components}]\n", 5)+"  - text: never reached\n")
	out, err := (&Loop{Provider: p, Tools: cat, MaxSteps: 3}).Run(context.Background(), "loop")
	if err != nil {
		t.Fatal(err)
	}
	if !out.Stopped || out.Steps != 3 || len(cat.called) != 3 || !strings.Contains(out.Text, "3 steps") {
		t.Errorf("outcome = %+v, called %d", out, len(cat.called))
	}
}

// A call the transport cannot make (the token revoked, the server gone) ends
// the run with the error, rather than feeding it to the model.
func TestLoopStopsOnTransportError(t *testing.T) {
	t.Parallel()
	cat, p := fleet(), replay(t, "turns:\n  - tool_calls: [{name: list-components}]\n  - text: ok\n")
	cat.fail = errors.New("unauthorized")
	if _, err := (&Loop{Provider: p, Tools: cat}).Run(context.Background(), "go"); err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("err = %v, want the transport's", err)
	}
}

func decisions(o Outcome) string {
	var s []string
	for _, c := range o.Calls {
		s = append(s, c.Tool+":"+string(c.Decision))
	}
	return strings.Join(s, ",")
}
