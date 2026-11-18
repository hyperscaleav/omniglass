package mcp

import (
	"context"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hyperscaleav/omniglass/internal/mcp/mcpgen"
	"github.com/hyperscaleav/omniglass/internal/mcp/skills"
)

// Every tool a skill names is in the catalog (#2026): a skill that names a tool
// the server does not serve would send an agent after nothing.
func TestEverySkillToolIsInTheCatalog(t *testing.T) {
	for _, s := range skills.All() {
		if miss := missingTools(s, catalog); len(miss) > 0 {
			t.Errorf("skill %s names tools the catalog lacks: %s", s.Name, strings.Join(miss, ", "))
		}
	}
}

// The check above catches a tool the catalog lacks.
func TestMissingToolsCatchesAnAbsentTool(t *testing.T) {
	s := skills.Skill{Name: "x", Tools: []string{"get-component", "acknowledge-nothing"}}
	if got := missingTools(s, catalog); !slices.Equal(got, []string{"acknowledge-nothing"}) {
		t.Errorf("missingTools = %v, want [acknowledge-nothing]", got)
	}
}

// The committed skills reference page is what the generator renders from the
// catalog and the embedded skills: a skill edited without `make gen` fails here.
func TestSkillsDocMatchesTheRegistry(t *testing.T) {
	got, err := os.ReadFile("../../docs/src/content/docs/reference/mcp/skills.md")
	if err != nil {
		t.Fatalf("read the skills page: %v (run make gen)", err)
	}
	if want := mcpgen.RenderSkills(catalog, skills.All()); string(got) != string(want) {
		t.Fatal("the skills page differs from the registry: run make gen")
	}
}

// The server lists every skill as a prompt with its arguments, and a prompt
// get answers the skill's workflow with the arguments given; a missing
// required argument and an unknown skill are refused.
func TestTheServerServesSkillsAsPrompts(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&fakeAPI{}, "test"))
	t.Cleanup(srv.Close)
	ctx := context.Background()
	cs, err := Dial(ctx, srv.URL+"/mcp", goodToken, &sdk.Implementation{Name: "t", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	assertServesSkills(t, cs)
	if info := cs.InitializeResult(); info == nil || !strings.Contains(info.Instructions, "triage-alarm") {
		t.Errorf("the instructions do not name the skills")
	}
}

// Over stdio, the relay serves the server's prompts too.
func TestRelayServesTheServersPrompts(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&fakeAPI{}, "test"))
	t.Cleanup(srv.Close)
	assertServesSkills(t, stdioClient(t, srv.URL+"/mcp", goodToken))
}

func assertServesSkills(t *testing.T, cs *sdk.ClientSession) {
	t.Helper()
	ctx := context.Background()
	var names []string
	for p, err := range cs.Prompts(ctx, nil) {
		if err != nil {
			t.Fatalf("list prompts: %v", err)
		}
		names = append(names, p.Name)
		if p.Name == "triage-alarm" && (len(p.Arguments) == 0 || p.Arguments[0].Name != "component" || !p.Arguments[0].Required) {
			t.Errorf("triage-alarm arguments = %+v", p.Arguments)
		}
	}
	var want []string
	for _, s := range skills.All() {
		want = append(want, s.Name)
	}
	if !slices.Equal(names, want) {
		t.Fatalf("prompts = %v, want %v", names, want)
	}
	res, err := cs.GetPrompt(ctx, &sdk.GetPromptParams{Name: "triage-alarm", Arguments: map[string]string{"component": "amp-9"}})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	if len(res.Messages) != 1 || res.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v", res.Messages)
	}
	text, _ := res.Messages[0].Content.(*sdk.TextContent)
	if text == nil || !strings.Contains(text.Text, "# Triage an alarm") || !strings.Contains(text.Text, "- component: amp-9") {
		t.Errorf("prompt text = %+v", res.Messages[0].Content)
	}
	if _, err := cs.GetPrompt(ctx, &sdk.GetPromptParams{Name: "triage-alarm"}); err == nil || !strings.Contains(err.Error(), "component") {
		t.Errorf("no required argument = %v, want a refusal naming it", err)
	}
	if _, err := cs.GetPrompt(ctx, &sdk.GetPromptParams{Name: "no-such-skill"}); err == nil {
		t.Error("an unknown prompt was served")
	}
}

// `omniglass mcp skill <name>` prints the SKILL.md (its test holds it to Raw),
// and a prompt get answers the workflow: the file is exactly its front matter
// and then the prompt's text, the arguments given listed after it under
// `## Given` (#2026, ADR point 1). A skill file and its prompt can never carry
// two different workflows.
func TestTheSkillFileIsItsFrontMatterThenThePrompt(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&fakeAPI{}, "test"))
	t.Cleanup(srv.Close)
	ctx := context.Background()
	cs, err := Dial(ctx, srv.URL+"/mcp", goodToken, &sdk.Implementation{Name: "t", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	for _, s := range skills.All() {
		args := map[string]string{}
		var given []string
		for _, a := range s.Arguments {
			if a.Required {
				args[a.Name] = "v-" + a.Name
				given = append(given, "- "+a.Name+": v-"+a.Name)
			}
		}
		res, err := cs.GetPrompt(ctx, &sdk.GetPromptParams{Name: s.Name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: get prompt: %v", s.Name, err)
		}
		text := res.Messages[0].Content.(*sdk.TextContent).Text
		workflow, tail, ok := strings.Cut(text, "\n\n## Given\n\n")
		if len(given) > 0 && (!ok || tail != strings.Join(given, "\n")+"\n") {
			t.Errorf("%s: the prompt does not end with the arguments given: %q", s.Name, tail)
		}
		raw := string(s.Raw)
		front, rest, ok := strings.Cut(strings.TrimPrefix(raw, "---\n"), "\n---\n")
		if !strings.HasPrefix(raw, "---\n") || !ok || front == "" {
			t.Fatalf("%s: the file does not open with its front matter", s.Name)
		}
		if strings.TrimRight(strings.TrimLeft(rest, "\n"), "\n") != workflow {
			t.Errorf("%s: the file after its front matter is not the prompt's workflow", s.Name)
		}
	}
}
