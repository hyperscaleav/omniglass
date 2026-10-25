package cli

import (
	"testing"

	"github.com/spf13/cobra"
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
