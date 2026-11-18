package mcp

import (
	"context"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hyperscaleav/omniglass/internal/mcp/skills"
	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
)

// addSkills serves every embedded skill (#2026) as an MCP prompt: the skill's
// arguments are the prompt's, and a get answers the skill's workflow as one
// user message, the arguments given appended. A prompt runs nothing: it is text
// that tells the agent which tools to call and in what order, and each of those
// calls is gated, scoped and audited as any other.
func addSkills(srv *sdk.Server) {
	for _, s := range skills.All() {
		s := s
		srv.AddPrompt(promptFor(s), func(_ context.Context, req *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
			var args map[string]string
			if req.Params != nil {
				args = req.Params.Arguments
			}
			text, err := s.Prompt(args)
			if err != nil {
				return nil, err
			}
			return &sdk.GetPromptResult{
				Description: s.Description,
				Messages:    []*sdk.PromptMessage{{Role: "user", Content: &sdk.TextContent{Text: text}}},
			}, nil
		})
	}
}

func promptFor(s skills.Skill) *sdk.Prompt {
	p := &sdk.Prompt{Name: s.Name, Description: s.Description}
	for _, a := range s.Arguments {
		p.Arguments = append(p.Arguments, &sdk.PromptArgument{Name: a.Name, Description: a.Description, Required: a.Required})
	}
	return p
}

// skillInstructions is the sentence the server's instructions end with: the
// skills it serves, so an agent knows to reach for one.
func skillInstructions() string {
	var names []string
	for _, s := range skills.All() {
		names = append(names, s.Name)
	}
	if len(names) == 0 {
		return ""
	}
	return " For a whole job, start from a skill, served as a prompt: " + strings.Join(names, ", ") + "."
}

// missingTools is the tools s names that defs does not hold.
func missingTools(s skills.Skill, defs []tooldef.Def) []string {
	have := map[string]bool{}
	for _, d := range defs {
		have[d.Name] = true
	}
	var miss []string
	for _, t := range s.Tools {
		if !have[t] {
			miss = append(miss, t)
		}
	}
	return miss
}
