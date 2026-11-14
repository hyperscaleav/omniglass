package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/cli/apiclient"
	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/spf13/cobra"
)

// The generated command tree (internal/cli/generated.go) calls into this
// hand-written runtime: runAPICommand issues the request and renders the result,
// and clientFromCmd resolves the connection from the persistent flags. Keeping
// the runtime here means regenerating the commands never regenerates the
// transport or the flag contract.

// jsonOrString coerces a structured body flag's string into the value the API
// expects: it parses the input as JSON so `--value 30` sends the number 30,
// `--value true` the boolean, and `--fields '{"k":"v"}'` the object. A string
// that is not valid JSON (a bare `HDMI1`) falls back to itself, so the common
// case needs no quoting; a string that looks like JSON (`30`, `true`) is quoted
// to force a string (`--value '"30"'`).
//
// Its reach narrowed with #711. A scalar the schema names now takes a flag of
// that type (an integer flag refuses `soon` at the shell), so this is emitted
// only where there is no flag type to carry the value: an object, an array, an
// untyped `any`, and a nullable number or boolean, whose `null` a typed flag
// could not send. See flagShape in cmd/cligen for the whole rule.
func jsonOrString(s string) any {
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return v
	}
	return s
}

// freeFormBody resolves the request body of a command whose schema is a free-form
// object with no declared properties (a merge patch, #1333): the document given
// inline as --body, or read from --body-file (a path, or - for stdin). It returns
// the document as written, compacted, so the wire carries exactly the keys the
// operator typed, null included (null on a key is how a merge patch restores it).
//
// Every refusal happens here, before runAPICommand, so a bad document never
// leaves the shell: both spellings at once, neither when the body is required,
// an unreadable file, malformed JSON, and a document that is not an object.
func freeFormBody(cmd *cobra.Command, inline, file string, required bool) (any, error) {
	hasInline, hasFile := cmd.Flags().Changed("body"), cmd.Flags().Changed("body-file")
	var raw []byte
	switch {
	case hasInline && hasFile:
		return nil, fmt.Errorf("give the body as --body or --body-file, not both")
	case hasInline:
		raw = []byte(inline)
	case hasFile:
		var err error
		if file == "-" {
			raw, err = io.ReadAll(cmd.InOrStdin())
		} else {
			raw, err = os.ReadFile(file)
		}
		if err != nil {
			return nil, fmt.Errorf("--body-file: %w", err)
		}
	case required:
		return nil, fmt.Errorf("a request body is required: pass a JSON object as --body, or --body-file (- for stdin)")
	default:
		return nil, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		if err == nil {
			err = fmt.Errorf("null is not an object")
		}
		return nil, fmt.Errorf("the request body is not a JSON object: %w", err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, fmt.Errorf("the request body is not valid JSON: %w", err)
	}
	return json.RawMessage(compact.Bytes()), nil
}

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
// flags, each falling back to its environment variable when it is not given.
func clientFromCmd(cmd *cobra.Command) *apiclient.Client {
	return apiclient.New(flagOrEnv(cmd, "server", "OMNIGLASS_SERVER"), flagOrEnv(cmd, "token", "OMNIGLASS_TOKEN"))
}

// defaultServer is where the CLI connects when neither --server nor
// OMNIGLASS_SERVER says otherwise.
const defaultServer = "http://localhost:8080"

// addClientFlags installs the shared connection flags on the root, so every
// generated command inherits them. Their defaults are fixed literals, never the
// environment: cobra prints a flag's default in every --help, in completion and
// in the generated CLI reference, so a default read from OMNIGLASS_TOKEN
// printed a live credential to anyone who asked for help (#979). The
// environment is read when the command runs (flagOrEnv), and help names the
// variable instead of its value.
func addClientFlags(root *cobra.Command) {
	root.PersistentFlags().String("server", defaultServer,
		"Omniglass server base URL; when omitted, $OMNIGLASS_SERVER, else the default")
	root.PersistentFlags().String("token", "",
		"bearer token; when omitted, $OMNIGLASS_TOKEN (read at run time, never printed)")
}

// flagOrEnv resolves a connection flag at run time: the flag when the operator
// gave it (an explicit empty value included), else the environment variable
// when it is set, else the flag's literal default.
func flagOrEnv(cmd *cobra.Command, flag, env string) string {
	v, _ := cmd.Flags().GetString(flag)
	if cmd.Flags().Changed(flag) {
		return v
	}
	if e := config.Get(env); e != "" {
		return e
	}
	return v
}

// pathSegment renders a positional argument as exactly one URL path segment, the
// way every generated command builds its request path. url.PathEscape already
// encodes a slash, so a qualified name (`<component>/<endpoint>`,
// `<owner>/<name>`, #1133) stays one segment; a colon it leaves alone, and the
// router reads the first colon in a segment as the start of a `:verb` custom
// method, so a filename carrying one would never reach its route. Encoding it too
// keeps the segment whole, and the server decodes both back. No name or dotted
// address carries either character, so for them this is url.PathEscape.
func pathSegment(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), ":", "%3A")
}

// pathArgs is every generated command's positional check: exactly n arguments,
// none of them a dot segment. A positional rides the request path as one
// segment, and `.` or `..` there is a segment a normalizing hop (a proxy, an
// ingress) rewrites into the route above it, so `member delete night-shift ..`
// could reach `DELETE /principal-groups/night-shift`. No name, dotted address or
// filename is either (the name rule opens on a letter or digit, and a file may
// not be named one, #1133), so refusing them loses nothing.
func pathArgs(n int) cobra.PositionalArgs {
	exact := cobra.ExactArgs(n)
	return func(cmd *cobra.Command, args []string) error {
		if err := exact(cmd, args); err != nil {
			return err
		}
		for _, a := range args {
			if a == "." || a == ".." {
				return fmt.Errorf("argument %q is a dot segment, which addresses no resource", a)
			}
		}
		return nil
	}
}
