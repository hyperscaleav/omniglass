package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/hyperscaleav/omniglass/internal/mcp"
)

// newMCPCmd is `omniglass mcp` (#1162): the stdio face of the server's MCP
// endpoint, for an agent that launches its MCP servers as local commands. It
// relays to the server's /mcp with the --server and --token connection (which
// default from OMNIGLASS_SERVER and OMNIGLASS_TOKEN), so the agent drives the
// same generated tools, with the same token's permissions, as one connecting
// over HTTP. `tools` and `call` are a thin stdio client of it, for scripts and
// demos.
func newMCPCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the omniglass MCP tools over stdio, for a local agent",
		Long: "Serves the omniglass MCP server over stdio by relaying to the server's /mcp endpoint with your " +
			"token, so a local agent (Claude Code, or any MCP client that launches a command) drives omniglass " +
			"through the same generated tools, with your permissions and scope, audited as you via MCP. Configure " +
			"the agent to run `omniglass mcp` with OMNIGLASS_SERVER and OMNIGLASS_TOKEN in its environment. " +
			"Standard output carries only the MCP protocol; anything else goes to standard error.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			server, token := connection(cmd)
			return mcp.Relay(cmd.Context(), os.Stdin, os.Stdout, endpointOf(server), token, version)
		},
	}
	cmd.AddCommand(newMCPToolsCmd(version), newMCPCallCmd(version))
	return cmd
}

func newMCPToolsCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "List the MCP tools, as an MCP client over stdio sees them",
		Long: "Lists every tool the omniglass MCP server serves, with what a client may assume of each (read-only, " +
			"destructive, idempotent, open-world), by connecting over stdio to `omniglass mcp` exactly as an agent does.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cs, err := stdioSession(cmd, version)
			if err != nil {
				return err
			}
			defer cs.Close()
			type listed struct {
				Name        string `json:"name"`
				Title       string `json:"title"`
				ReadOnly    bool   `json:"read_only"`
				Destructive bool   `json:"destructive"`
				Idempotent  bool   `json:"idempotent"`
				OpenWorld   bool   `json:"open_world"`
			}
			out := []listed{}
			for t, err := range cs.Tools(cmd.Context(), nil) {
				if err != nil {
					return err
				}
				l := listed{Name: t.Name, Title: t.Title}
				if a := t.Annotations; a != nil {
					l.ReadOnly, l.Idempotent = a.ReadOnlyHint, a.IdempotentHint
					l.Destructive = a.DestructiveHint == nil || *a.DestructiveHint
					l.OpenWorld = a.OpenWorldHint == nil || *a.OpenWorldHint
				}
				out = append(out, l)
			}
			return printJSON(cmd, out)
		},
	}
}

func newMCPCallCmd(version string) *cobra.Command {
	var args string
	cmd := &cobra.Command{
		Use:   "call <tool>",
		Short: "Call one MCP tool, as an MCP client over stdio does",
		Long: "Calls one tool on the omniglass MCP server, connecting over stdio to `omniglass mcp` exactly as an agent " +
			"does, and prints its result. A tool error (the route's own refusal) is printed and exits non-zero.",
		Example: `  omniglass mcp call get-component-reachability --args '{"name":"panel-1"}'`,
		Args:    pathArgs(1),
		RunE: func(cmd *cobra.Command, pos []string) error {
			var arguments map[string]any
			if strings.TrimSpace(args) != "" {
				if err := json.Unmarshal([]byte(args), &arguments); err != nil {
					return fmt.Errorf("--args must be a JSON object: %w", err)
				}
			}
			cs, err := stdioSession(cmd, version)
			if err != nil {
				return err
			}
			defer cs.Close()
			res, err := cs.CallTool(cmd.Context(), &sdk.CallToolParams{Name: pos[0], Arguments: arguments})
			if err != nil {
				return err
			}
			var text strings.Builder
			for _, c := range res.Content {
				if tc, ok := c.(*sdk.TextContent); ok {
					text.WriteString(tc.Text)
				}
			}
			var pretty bytes.Buffer
			if json.Indent(&pretty, []byte(text.String()), "", "  ") == nil {
				fmt.Fprintln(cmd.OutOrStdout(), pretty.String())
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), text.String())
			}
			if res.IsError {
				return errors.New("the tool returned an error")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&args, "args", "", "the tool's arguments, a JSON object")
	return cmd
}

// stdioSession connects an MCP client to `omniglass mcp` launched as a child,
// over its stdio: the path a local agent takes. The connection reaches the
// child through its environment, never its arguments, so the token is not on a
// process listing.
func stdioSession(cmd *cobra.Command, version string) (*sdk.ClientSession, error) {
	server, token := connection(cmd)
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate the omniglass binary: %w", err)
	}
	child := exec.CommandContext(cmd.Context(), exe, "mcp")
	child.Env = append(os.Environ(), "OMNIGLASS_SERVER="+server, "OMNIGLASS_TOKEN="+token)
	child.Stderr = os.Stderr
	cl := sdk.NewClient(&sdk.Implementation{Name: "omniglass-cli", Version: version}, nil)
	cs, err := cl.Connect(cmd.Context(), &sdk.CommandTransport{Command: child}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to omniglass mcp over stdio: %w", err)
	}
	return cs, nil
}

// connection is the --server and --token pair every client command shares: a
// flag the operator gave, else its environment variable, read now (never from a
// flag default, which --help would print), else the flag's literal default.
func connection(cmd *cobra.Command) (server, token string) {
	resolve := func(flag, env string) string {
		v, _ := cmd.Flags().GetString(flag)
		if cmd.Flags().Changed(flag) {
			return v
		}
		if e := config.Get(env); e != "" {
			return e
		}
		return v
	}
	return resolve("server", "OMNIGLASS_SERVER"), resolve("token", "OMNIGLASS_TOKEN")
}

// endpointOf is the MCP endpoint of a server root.
func endpointOf(server string) string {
	return strings.TrimRight(server, "/") + "/mcp"
}

func printJSON(cmd *cobra.Command, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(b))
	return nil
}
