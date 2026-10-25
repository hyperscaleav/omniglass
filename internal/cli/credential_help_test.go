package cli

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// #979: every command's --help printed the live bearer token, because --token
// took OMNIGLASS_TOKEN as its flag default and cobra prints a flag's default in
// help, in the CLI reference docsgen renders, and anywhere else a usage string
// goes. An integrator pasting --help into a ticket leaked a working credential.
// The connection flags now resolve the environment at run time, and these tests
// hold every surface that renders a flag to showing where a value comes from,
// never the value.

// envSentinels sets every variable the binary reads (config.Registry) to a
// value only the environment could have supplied, and returns them by key. The
// token is a real one, minted the way bootstrap mints it, so the test holds the
// shape an operator actually exports.
func envSentinels(t *testing.T) map[string]string {
	t.Helper()
	tok, _, _, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint a token: %v", err)
	}
	s := make(map[string]string, len(config.Registry))
	for _, v := range config.Registry {
		s[v.Key] = "sentinel-" + strings.ToLower(v.Key) + "-q7x"
	}
	s["OMNIGLASS_TOKEN"] = tok
	s["OMNIGLASS_NODE_TOKEN"] = "ogp_nodetokensentinel_" + strings.Repeat("z", 24)
	s["OMNIGLASS_SERVER"] = "https://operator:hunter2-sentinel@omniglass.example"
	for k, v := range s {
		t.Setenv(k, v)
	}
	return s
}

// leaked names every variable whose value appears in out.
func leaked(out string, sentinels map[string]string) []string {
	var keys []string
	for k, v := range sentinels {
		if strings.Contains(out, v) {
			keys = append(keys, "$"+k)
		}
	}
	sort.Strings(keys)
	return keys
}

// fullTree is the assembled root writing to out, with cobra's lazily added
// commands (help and completion) attached, so a walk reaches every command an
// operator can type. The writer is set first because the completion commands
// capture the root's writer when they are added, as Execute adds them.
func fullTree(out io.Writer) *cobra.Command {
	root := Root("test")
	root.SetOut(out)
	root.SetErr(out)
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	return root
}

// commandPaths is every command in the tree, the root included, as the
// argument vector that reaches it.
func commandPaths(root *cobra.Command) [][]string {
	var out [][]string
	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		out = append(out, path)
		for _, sub := range c.Commands() {
			walk(sub, append(append([]string{}, path...), sub.Name()))
		}
	}
	walk(root, nil)
	return out
}

// execute runs one invocation against a freshly assembled tree, as the binary
// would, and returns everything it wrote to stdout and stderr.
func execute(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	root := fullTree(&out)
	root.SetArgs(args)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Errorf("omniglass %s: %v\n%s", strings.Join(args, " "), err, out.String())
	}
	return out.String()
}

func join(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// TestNoHelpShowsACredential walks the whole command tree with a token (and
// every other variable the binary reads) in the environment and renders every
// surface that prints a flag: each command's --help, the help command's page for
// it, shell completion of its flag names with their descriptions, and
// completion of the --token value. None of them may carry a value the
// environment supplied.
func TestNoHelpShowsACredential(t *testing.T) {
	sentinels := envSentinels(t)
	paths := commandPaths(fullTree(io.Discard))
	if len(paths) < 100 {
		t.Fatalf("walked %d commands; the generated tree has hundreds, so the walk is broken", len(paths))
	}
	for _, p := range paths {
		for _, args := range [][]string{
			join(p, []string{"--help"}),
			join([]string{"help"}, p),
			join([]string{cobra.ShellCompRequestCmd}, p, []string{"--"}),
			join([]string{cobra.ShellCompRequestCmd}, p, []string{"--token", ""}),
		} {
			if keys := leaked(execute(t, args...), sentinels); len(keys) > 0 {
				t.Errorf("omniglass %s prints the value of %s", strings.Join(args, " "), strings.Join(keys, ", "))
			}
		}
	}
	// The completion scripts themselves, which a shell sources at startup.
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		if keys := leaked(execute(t, "completion", shell), sentinels); len(keys) > 0 {
			t.Errorf("omniglass completion %s prints the value of %s", shell, strings.Join(keys, ", "))
		}
	}
}

// TestHelpNamesWhereTheTokenComesFrom is the other half of the fix: help that
// hides the value still says where it is read from, on a generated command (the
// inherited global flags) and on the root (where the flags are declared).
func TestHelpNamesWhereTheTokenComesFrom(t *testing.T) {
	envSentinels(t)
	for _, args := range [][]string{{"component", "list", "--help"}, {"--help"}} {
		out := execute(t, args...)
		for _, want := range []string{"$OMNIGLASS_TOKEN", "$OMNIGLASS_SERVER"} {
			if !strings.Contains(out, want) {
				t.Errorf("omniglass %s does not name %s as a source:\n%s", strings.Join(args, " "), want, out)
			}
		}
	}
}

// TestNoFlagDefaultReadsTheEnvironment is the structural guard: no flag
// anywhere in the tree, hidden ones included, takes a default (or a usage
// string, or a no-value default) from the environment. A flag's default is
// printed by every help template, the CLI reference and completion; a value the
// environment supplies is resolved at run time (flagOrEnv) instead, so a
// credential can reach the command without ever being rendered.
func TestNoFlagDefaultReadsTheEnvironment(t *testing.T) {
	sentinels := envSentinels(t)
	root := fullTree(io.Discard)
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		check := func(f *pflag.Flag) {
			for _, field := range []struct{ name, val string }{
				{"default", f.DefValue}, {"usage", f.Usage}, {"no-value default", f.NoOptDefVal},
			} {
				if keys := leaked(field.val, sentinels); len(keys) > 0 {
					t.Errorf("%s --%s: its %s carries the value of %s; resolve the variable at run time, never as the flag's default",
						c.CommandPath(), f.Name, field.name, strings.Join(keys, ", "))
				}
			}
		}
		c.LocalFlags().VisitAll(check)
		c.PersistentFlags().VisitAll(check)
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)

	// A flag default read through os.Getenv would dodge the sentinels above,
	// which only see the declared registry. Every env read in the command
	// surface goes through config.Get, so the registry sweep is complete.
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	envReads := map[string]map[string]bool{
		"os":      {"Getenv": true, "LookupEnv": true, "Environ": true, "ExpandEnv": true},
		"syscall": {"Getenv": true, "Environ": true},
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && envReads[pkg.Name][sel.Sel.Name] {
				t.Errorf("%s: %s.%s reads the environment around config.Get, out of this guard's sight",
					fset.Position(sel.Pos()), pkg.Name, sel.Sel.Name)
			}
			return true
		})
	}
}

// TestTheTokenStillComesFromTheEnvironment pins that the fix moved the read,
// not the feature: a generated command, run with no connection flags, reaches
// $OMNIGLASS_SERVER carrying $OMNIGLASS_TOKEN, a flag still wins over the
// environment, and an explicitly empty --token still sends no credential.
func TestTheTokenStillComesFromTheEnvironment(t *testing.T) {
	var mu sync.Mutex
	var gotAuth string
	var hits int
	seen := func() (string, int) {
		mu.Lock()
		defer mu.Unlock()
		return gotAuth, hits
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	t.Setenv("OMNIGLASS_SERVER", srv.URL)
	t.Setenv("OMNIGLASS_TOKEN", "ogp_fromtheenv")

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"the environment alone", []string{"auth", "me"}, "Bearer ogp_fromtheenv"},
		{"a flag over the environment", []string{"auth", "me", "--token", "ogp_fromtheflag"}, "Bearer ogp_fromtheflag"},
		{"an explicitly empty flag", []string{"auth", "me", "--token", ""}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, before := seen()
			execute(t, tc.args...)
			auth, after := seen()
			if after != before+1 {
				t.Fatalf("the request never reached $OMNIGLASS_SERVER (%d hits)", after-before)
			}
			if auth != tc.want {
				t.Errorf("Authorization = %q, want %q", auth, tc.want)
			}
		})
	}

	// And --server wins over the environment the same way.
	var otherAuth string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		otherAuth = r.Header.Get("Authorization")
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer other.Close()
	_, before := seen()
	execute(t, "auth", "me", "--server", other.URL)
	mu.Lock()
	defer mu.Unlock()
	if otherAuth != "Bearer ogp_fromtheenv" || hits != before {
		t.Errorf("--server did not win over $OMNIGLASS_SERVER: other got %q, the env server %d more hit(s)", otherAuth, hits-before)
	}
}
