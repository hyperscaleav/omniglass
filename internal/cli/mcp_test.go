package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/hyperscaleav/omniglass/internal/mcp/skills"
)

// `omniglass mcp` takes its connection from a flag the operator gave, else the
// environment read when it runs, never from a flag default (which --help would
// print, #979): with the flags left at empty defaults, the environment answers;
// a flag given on the line wins over it.
func TestMCPConnectionReadsTheEnvironmentAtRunTime(t *testing.T) {
	t.Setenv("OMNIGLASS_SERVER", "https://og.example")
	t.Setenv("OMNIGLASS_TOKEN", "ogp_00000000_fromtheenvironment")
	cmd := &cobra.Command{Use: "probe", Run: func(*cobra.Command, []string) {}}
	cmd.Flags().String("server", "http://localhost:8080", "")
	cmd.Flags().String("token", "", "")

	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatal(err)
	}
	server, token := connection(cmd)
	if server != "https://og.example" || token != "ogp_00000000_fromtheenvironment" {
		t.Errorf("connection() = %q, %q, want the environment's", server, token)
	}

	if err := cmd.ParseFlags([]string{"--token", "ogp_11111111_fromtheflag", "--server", "http://127.0.0.1:9"}); err != nil {
		t.Fatal(err)
	}
	server, token = connection(cmd)
	if server != "http://127.0.0.1:9" || token != "ogp_11111111_fromtheflag" {
		t.Errorf("connection() = %q, %q, want the flags'", server, token)
	}
	if got := endpointOf("https://og.example/"); got != "https://og.example/mcp" {
		t.Errorf("endpointOf = %q", got)
	}
}

// `omniglass mcp skill` lists the embedded skills, and `omniglass mcp skill
// <name>` prints one exactly as its SKILL.md reads, so an agent that loads
// skill files can take it as is (#2026). It needs no server: the skills ship
// in the binary.
func TestMCPSkillListsAndPrintsTheSkills(t *testing.T) {
	run := func(args ...string) (string, error) {
		cmd := newMCPSkillCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}
	list, err := run()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listed []struct{ Name, Category, Description string }
	if err := json.Unmarshal([]byte(list), &listed); err != nil || len(listed) != len(skills.All()) {
		t.Fatalf("list = %s (%v)", list, err)
	}
	if listed[0].Name == "" || listed[0].Category == "" || listed[0].Description == "" {
		t.Errorf("listed = %+v", listed[0])
	}
	one, err := run("triage-alarm")
	if err != nil {
		t.Fatalf("print: %v", err)
	}
	s, _ := skills.Get("triage-alarm")
	if one != string(s.Raw) {
		t.Errorf("printed skill is not the SKILL.md")
	}
	if _, err := run("no-such-skill"); err == nil || !strings.Contains(err.Error(), "triage-alarm") {
		t.Errorf("unknown skill = %v, want a refusal listing the skills", err)
	}
}
