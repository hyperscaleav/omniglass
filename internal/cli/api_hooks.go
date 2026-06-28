package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/hyperscaleav/omniglass/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

// The generated command tree (internal/cli/generated.go) calls into this
// hand-written runtime: runAPICommand issues the request and renders the result,
// and clientFromCmd resolves the connection from the persistent flags. Keeping
// the runtime here means regenerating the commands never regenerates the
// transport or the flag contract.

// runAPICommand issues one API call for a generated command, prints the JSON
// response to stdout (pretty-printed when it parses), and maps a non-2xx status
// to a non-zero exit by returning an error after showing the server's message.
func runAPICommand(cmd *cobra.Command, method, path string, body any) error {
	res, err := clientFromCmd(cmd).Do(cmd.Context(), method, path, body)
	if err != nil {
		return err
	}
	if len(res.Body) > 0 {
		var pretty bytes.Buffer
		if json.Indent(&pretty, res.Body, "", "  ") == nil {
			fmt.Fprintln(cmd.OutOrStdout(), pretty.String())
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), string(res.Body))
		}
	}
	if !res.OK() {
		return fmt.Errorf("server returned status %d", res.Status)
	}
	return nil
}

// clientFromCmd builds an apiclient from the --server and --token persistent
// flags (which default from OMNIGLASS_SERVER and OMNIGLASS_TOKEN).
func clientFromCmd(cmd *cobra.Command) *apiclient.Client {
	server, _ := cmd.Flags().GetString("server")
	token, _ := cmd.Flags().GetString("token")
	return apiclient.New(server, token)
}

// addClientFlags installs the shared connection flags on the root, so every
// generated command inherits them. Defaults come from the environment.
func addClientFlags(root *cobra.Command) {
	server := os.Getenv("OMNIGLASS_SERVER")
	if server == "" {
		server = "http://localhost:8080"
	}
	root.PersistentFlags().String("server", server, "Omniglass server base URL (env OMNIGLASS_SERVER)")
	root.PersistentFlags().String("token", os.Getenv("OMNIGLASS_TOKEN"), "bearer token (env OMNIGLASS_TOKEN)")
}
