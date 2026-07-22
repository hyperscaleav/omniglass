// Command cligen generates the cobra command tree for the CLI from the committed
// OpenAPI document (api/openapi.json), the second stage of the generation
// pipeline after the spec itself. It is run by `make gen`. Every API operation
// becomes a command: the resource and verb come from the AIP-style path and
// method (a `:verb` custom method maps to its own subcommand), path parameters
// become positional args, the request body becomes --flags, and the help and
// example come from the operation summary and description. The output,
// internal/cli/generated.go, is committed and reviewed like any code; the
// hand-written commands (bootstrap, server, migrate) live elsewhere and compose
// with the generated tree on the same root. The runtime the generated tree calls
// (the client and connection flags) is the hand-written api_hooks.go.
package main

import (
	"encoding/json"
	"fmt"
	"go/format"
	"log"
	"os"
	"sort"
	"strings"
	"text/template"
)

func main() {
	raw, err := os.ReadFile("api/openapi.json")
	if err != nil {
		log.Fatalf("cligen: read spec: %v", err)
	}
	var doc spec
	if err := json.Unmarshal(raw, &doc); err != nil {
		log.Fatalf("cligen: parse spec: %v", err)
	}

	base := "/api/v1"
	if len(doc.Servers) > 0 && doc.Servers[0].URL != "" {
		base = strings.TrimRight(doc.Servers[0].URL, "/")
	}

	cmds := buildCommands(doc, base)
	roots := tree(cmds)

	out, err := render(roots)
	if err != nil {
		log.Fatalf("cligen: render: %v", err)
	}
	formatted, err := format.Source(out)
	if err != nil {
		log.Fatalf("cligen: gofmt: %v\n%s", err, out)
	}
	const dst = "internal/cli/api_gen.go"
	if err := os.WriteFile(dst, formatted, 0o644); err != nil {
		log.Fatalf("cligen: write: %v", err)
	}
	log.Printf("wrote %s (%d commands)", dst, len(cmds))
}

// --- OpenAPI model (only the fields the generator needs) --------------------

type spec struct {
	Servers    []server            `json:"servers"`
	Paths      map[string]pathItem `json:"paths"`
	Components components          `json:"components"`
}

type server struct {
	URL string `json:"url"`
}

type pathItem map[string]operation

type operation struct {
	OperationID string       `json:"operationId"`
	Summary     string       `json:"summary"`
	Description string       `json:"description"`
	Parameters  []param      `json:"parameters"`
	RequestBody *requestBody `json:"requestBody"`
}

type param struct {
	Name        string      `json:"name"`
	In          string      `json:"in"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
	Schema      paramSchema `json:"schema"`
}

type paramSchema struct {
	Type string `json:"type"`
}

type requestBody struct {
	Content map[string]struct {
		Schema struct {
			Ref string `json:"$ref"`
		} `json:"schema"`
	} `json:"content"`
}

type components struct {
	Schemas map[string]schema `json:"schemas"`
}

type schema struct {
	Required   []string            `json:"required"`
	Properties map[string]property `json:"properties"`
}

type property struct {
	Description string   `json:"description"`
	Type        jsonType `json:"type"`
}

// jsonType is an OpenAPI `type` that may be a single string ("string") or, for a
// nullable schema, an array (`["array", "null"]`). It resolves to the first
// non-null member, or "" for an absent/untyped `any`.
type jsonType string

func (t *jsonType) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = jsonType(s)
		return nil
	}
	var arr []string
	if json.Unmarshal(b, &arr) == nil {
		for _, x := range arr {
			if x != "null" {
				*t = jsonType(x)
				return nil
			}
		}
	}
	return nil // an absent or unexpected shape resolves to the untyped ""
}

var httpMethods = []string{"get", "post", "put", "patch", "delete"}

// --- command model ----------------------------------------------------------

type command struct {
	Words   []string // command path, e.g. ["location", "get"] or ["healthz"]
	Method  string   // GET, POST, ...
	APIPath string   // Go format template, e.g. /api/v1/locations/%s
	Args    []string // positional arg names, in path order
	Body    []bodyField
	Query   []queryField // OpenAPI query parameters, as optional flags
	Short   string
	Long    string
	Example string
}

type bodyField struct {
	Name     string
	Flag     string // hyphenated flag name
	Var      string // generated Go variable name
	Desc     string
	Required bool
	// JSON marks a field whose value is not a plain scalar (an object, an array,
	// or an untyped `any`): its flag still takes a string, but the string is
	// parsed as JSON (with a bare-string fallback) before it enters the request
	// body, so `--value 30` sends the number 30 and `--fields '{"k":"v"}'` sends
	// the object, not their quoted-string forms.
	JSON bool
}

// queryField is one OpenAPI query parameter surfaced as a cobra flag. The flag
// is optional by default: only a flag the operator actually sets is appended to
// the request query string, so an unset param keeps the server default.
type queryField struct {
	Name     string // OpenAPI param name (snake_case), the query-string key
	Flag     string // kebab-case flag name
	Var      string // generated Go variable name
	GoType   string // Go flag variable type: string, bool, int
	FlagFunc string // cobra flag setter: StringVar, BoolVar, IntVar
	Zero     string // Go source for the flag default: "", false, 0
	Desc     string
	Required bool
}

// nameOverride names an operation the route cannot. With the parent-qualified
// rule in commandWords, that is now only the **non-AIP** routes: the /auth family
// is a session surface rather than a resource collection, so deriving from its
// path gives `auth me session list` where an operator expects `session list`.
//
// This list used to hold fifty entries and every one of them was working around
// the old leaf-noun rule (its comments each said so: sessions, members, types,
// properties, all collapsing into one group). Those are gone, because the rule
// now does what they were doing by hand. An entry here should be rare and should
// say what is irregular about the ROUTE, never what collides.
var nameOverride = map[string]([]string){
	"get-healthz":             {"healthz"},
	"get-auth-me":             {"auth", "me"},
	"update-auth-me":          {"auth", "update-profile"},
	"change-auth-me-password": {"auth", "change-password"},
	"remove-auth-me-avatar":   {"auth", "remove-avatar"},
	"set-auth-me-avatar":      {"auth", "set-avatar"},
	"stop-impersonation":      {"auth", "stop-impersonation"},
	"get-auth-me-avatar":      {"auth", "avatar"},
	"create-auth-me-token":    {"auth", "create-token"},
	// The self-service session surface: an operator manages their own sessions,
	// so these are `session <verb>` rather than four levels under /auth/me.
	"list-auth-me-sessions":       {"session", "list"},
	"revoke-auth-me-session":      {"session", "revoke"},
	"revoke-all-auth-me-sessions": {"session", "revoke-all"},
	"login":                       {"auth", "login"},
	"logout":                      {"auth", "logout"},
}

func buildCommands(doc spec, base string) []command {
	var cmds []command
	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		item := doc.Paths[p]
		for _, method := range httpMethods {
			op, ok := item[method]
			if !ok {
				continue
			}
			cmds = append(cmds, buildCommand(doc, base, p, method, op))
		}
	}
	return cmds
}

func buildCommand(doc spec, base, path, method string, op operation) command {
	words := commandWords(path, method, op.OperationID)

	// Path params become positional args and %s slots, in path order. A custom
	// method fuses the param and the verb in one segment ({id}:archive); the param
	// still binds as a positional arg and the ":verb" suffix stays literal.
	apiPath := base
	var args []string
	for _, seg := range splitPath(path) {
		if name, suffix, ok := pathParamSeg(seg); ok {
			apiPath += "/%s" + suffix
			args = append(args, name)
		} else {
			apiPath += "/" + seg
		}
	}

	c := command{
		Words:   words,
		Method:  strings.ToUpper(method),
		APIPath: apiPath,
		Args:    args,
		Short:   op.Summary,
		Long:    op.Description,
	}
	c.Body = bodyFields(doc, op)
	c.Query = queryFields(op)
	c.Example = example(words, args, c.Body)
	return c
}

// queryFields maps an operation's OpenAPI query parameters (in: query) to
// optional cobra flags, in spec order. The OpenAPI type selects the flag setter
// (string -> StringVar, boolean -> BoolVar, integer -> IntVar); any other type
// falls back to a string flag. Path and other parameter locations are ignored.
func queryFields(op operation) []queryField {
	var out []queryField
	for _, p := range op.Parameters {
		if p.In != "query" {
			continue
		}
		goType, flagFunc, zero := "string", "StringVar", `""`
		switch p.Schema.Type {
		case "boolean":
			goType, flagFunc, zero = "bool", "BoolVar", "false"
		case "integer":
			goType, flagFunc, zero = "int", "IntVar", "0"
		}
		out = append(out, queryField{
			Name:     p.Name,
			Flag:     strings.ReplaceAll(p.Name, "_", "-"),
			Var:      "q" + goIdent(p.Name),
			GoType:   goType,
			FlagFunc: flagFunc,
			Zero:     zero,
			Desc:     p.Description,
			Required: p.Required,
		})
	}
	return out
}

// commandWords derives the cobra command path from the route, and it is the
// whole naming rule: **every collection segment contributes a level**, so a
// subresource is always addressed under the resource that owns it
// (`/components/{name}/properties` is `component property list`), and the verb
// is last.
//
// This replaces a rule that used only the collection nearest the leaf. That rule
// could not distinguish two parents ending in the same noun, so
// `/principals/{id}/grants` and `/principal-groups/{id}/grants` both became
// `grant create` and one silently shadowed the other. Across the 195 operations
// it produced 24 collisions (`property list` seven ways), each of which had to
// be found by a person typing it and then patched by hand in nameOverride. The
// overrides were the rule, written out fifty times.
//
// Qualifying by the parent produces no collisions, and it does so structurally:
// a command's name depends only on its own route, never on which other routes
// happen to exist, so adding a route can never rename an existing command.
func commandWords(path, method, opID string) []string {
	if w, ok := nameOverride[opID]; ok {
		return w
	}
	segs := splitPath(path)
	last := segs[len(segs)-1]

	// A custom method's verb is explicit; otherwise the HTTP method plus whether
	// the route addresses an item or a collection decides it.
	verb := ""
	if i := strings.Index(last, ":"); i >= 0 {
		verb = last[i+1:]
		segs[len(segs)-1] = last[:i]
		last = segs[len(segs)-1]
	}
	if verb == "" {
		if _, isItem := pathParam(last); isItem {
			switch method {
			case "patch", "put":
				verb = "update"
			case "delete":
				verb = "delete"
			default:
				verb = "get"
			}
		} else if method == "post" {
			verb = "create"
		} else {
			verb = "list"
		}
	}

	// The nouns are the collection segments: an {id} addresses an instance of the
	// collection before it and adds no level. A custom method on a collection
	// (`/sessions:revokeAll`) leaves that collection as the final noun.
	var nouns []string
	for _, sg := range segs {
		if _, isParam := pathParam(sg); isParam {
			continue
		}
		nouns = append(nouns, singular(sg))
	}
	return append(nouns, verb)
}

func bodyFields(doc spec, op operation) []bodyField {
	if op.RequestBody == nil {
		return nil
	}
	ct, ok := op.RequestBody.Content["application/json"]
	if !ok {
		return nil
	}
	name := strings.TrimPrefix(ct.Schema.Ref, "#/components/schemas/")
	sc, ok := doc.Components.Schemas[name]
	if !ok {
		return nil
	}
	required := map[string]bool{}
	for _, r := range sc.Required {
		required[r] = true
	}
	var props []string
	for k := range sc.Properties {
		if k == "$schema" { // Huma's meta property, not an operator field
			continue
		}
		props = append(props, k)
	}
	sort.Strings(props)

	var fields []bodyField
	for _, k := range props {
		// A string property passes through as its flag string; every other type
		// (boolean, number, integer, object, array, or an untyped `any` value) is
		// JSON-parsed so a typed or structured value survives the wire. A string is
		// the sole passthrough so a value that looks like JSON (a name `30`, a label
		// `true`) stays a string; a `--propagates false` becomes a JSON boolean, not
		// the string "false" a boolean body field would reject.
		raw := string(sc.Properties[k].Type) == "string"
		fields = append(fields, bodyField{
			Name:     k,
			Flag:     strings.ReplaceAll(k, "_", "-"),
			Var:      "f" + goIdent(k),
			Desc:     sc.Properties[k].Description,
			Required: required[k],
			JSON:     !raw,
		})
	}
	return fields
}

func example(words, args []string, body []bodyField) string {
	parts := append([]string{"omniglass"}, words...)
	for _, a := range args {
		parts = append(parts, "<"+a+">")
	}
	for _, f := range body {
		if f.Required {
			parts = append(parts, "--"+f.Flag+" "+f.Name)
		}
	}
	return "  " + strings.Join(parts, " ")
}

// --- grouping ---------------------------------------------------------------

// node is one level of the command tree. A node may carry a command of its own,
// have children, or both (`type location list` makes `type` a pure group and
// `location` a group under it). Cmd is nil for a pure group.
type node struct {
	Name string
	Cmd  *command
	Kids []*node
}

// HasCmd and HasKids keep the template free of nil checks.
func (n *node) HasCmd() bool  { return n.Cmd != nil }
func (n *node) HasKids() bool { return len(n.Kids) > 0 }

// Use is the cobra Use string for this node: its own name plus any positional
// placeholders when it carries a command.
func (n *node) Use() string {
	use := n.Name
	if n.Cmd != nil {
		for _, a := range n.Cmd.Args {
			use += " <" + a + ">"
		}
	}
	return use
}

// tree builds the command forest from each command's full Words path. It is
// N-level on purpose: the previous version bucketed by Words[0] and used only
// the LAST word as the leaf name, so a three-word path like
// `component property list` rendered as `component list` and silently collided
// with the real one. Any depth the route implies is a real level here.
func tree(cmds []command) []*node {
	var roots []*node
	find := func(kids *[]*node, name string) *node {
		for _, k := range *kids {
			if k.Name == name {
				return k
			}
		}
		n := &node{Name: name}
		*kids = append(*kids, n)
		return n
	}
	for i := range cmds {
		c := cmds[i]
		cur := find(&roots, c.Words[0])
		for _, w := range c.Words[1:] {
			cur = find(&cur.Kids, w)
		}
		cur.Cmd = &c
	}
	var sortRec func(ns []*node)
	sortRec = func(ns []*node) {
		sort.Slice(ns, func(i, j int) bool { return ns[i].Name < ns[j].Name })
		for _, n := range ns {
			sortRec(n.Kids)
		}
	}
	sortRec(roots)
	return roots
}

// --- path helpers -----------------------------------------------------------

func splitPath(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func pathParam(seg string) (string, bool) {
	if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
		return seg[1 : len(seg)-1], true
	}
	return "", false
}

// pathParamSeg recognizes a path-parameter segment, including a custom-method
// segment where the param and the verb are fused ({id}:archive). It returns the
// param name and any trailing literal (":archive", or "" for a plain {id}). A
// non-param segment (a collection, or a collection:verb) returns ok == false.
func pathParamSeg(seg string) (name, suffix string, ok bool) {
	if !strings.HasPrefix(seg, "{") {
		return "", "", false
	}
	end := strings.Index(seg, "}")
	if end < 0 {
		return "", "", false
	}
	return seg[1:end], seg[end+1:], true
}

// notPlural are nouns the -s rule would mangle: they end in s without being
// plural. `status` became `statu`, which shipped as a real command. The list is
// the whole irregular set in use; a noun that needs more than this belongs in
// nameOverride.
var notPlural = map[string]bool{
	"status": true, "healthz": true, "me": true, "dns": true, "https": true,
}

// singular depluralizes an AIP collection noun (locations, properties). It is
// deliberately small: the route vocabulary is ours, so the irregulars are a
// known set rather than an open English problem.
func singular(s string) string {
	if notPlural[s] {
		return s
	}
	if strings.HasSuffix(s, "ies") && len(s) > 3 {
		return s[:len(s)-3] + "y" // properties -> property
	}
	if strings.HasSuffix(s, "s") && len(s) > 1 {
		return s[:len(s)-1]
	}
	return s
}

// goIdent turns a snake_case field into an exported-ish camel suffix for a Go
// variable name (display_name -> DisplayName).
func goIdent(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// leafUse is the cobra Use string for a child command: the verb plus <arg>
// placeholders.
func (c command) LeafUse() string {
	use := c.Words[len(c.Words)-1]
	for _, a := range c.Args {
		use += " <" + a + ">"
	}
	return use
}

// StandaloneUse is the cobra Use for a parentless command (its single word plus
// any args).
func (c command) StandaloneUse() string { return c.LeafUse() }

// --- rendering --------------------------------------------------------------

func render(roots []*node) ([]byte, error) {
	var b strings.Builder
	if err := tmpl.Execute(&b, roots); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

var tmpl = template.Must(template.New("gen").Funcs(template.FuncMap{
	"quote": func(s string) string { return fmt.Sprintf("%q", s) },
}).Parse(`// Code generated by cmd/cligen from api/openapi.json. DO NOT EDIT.

package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

// generatedCommands returns the API-backed command tree built from the OpenAPI.
// clientFor resolves the server and token from the invoking command at run time.
func generatedCommands() []*cobra.Command {
	var roots []*cobra.Command
{{range $n := .}}
	roots = append(roots, {{template "node" $n}})
{{- end}}
	return roots
}

{{define "node"}}
	{{- if .HasCmd}}func() *cobra.Command {
		cmd := {{template "leaf" .Cmd}}
		{{- range $k := .Kids}}
		cmd.AddCommand({{template "node" $k}})
		{{- end}}
		return cmd
	}(){{else}}func() *cobra.Command {
		parent := &cobra.Command{
			Use:   {{quote .Name}},
			Short: {{quote (printf "Commands for the %s resource" .Name)}},
		}
		{{- range $k := .Kids}}
		parent.AddCommand({{template "node" $k}})
		{{- end}}
		return parent
	}(){{end}}
{{- end}}

{{define "leaf"}}func() *cobra.Command {
		{{- range $f := .Body}}
		var {{$f.Var}} string
		{{- end}}
		{{- range $q := .Query}}
		var {{$q.Var}} {{$q.GoType}}
		{{- end}}
		cmd := &cobra.Command{
			Use:     {{quote .LeafUse}},
			Short:   {{quote .Short}},
			Long:    {{quote .Long}},
			Example: {{quote .Example}},
			Args:    cobra.ExactArgs({{len .Args}}),
			RunE: func(cmd *cobra.Command, args []string) error {
				path := fmt.Sprintf({{quote .APIPath}}{{range $i, $a := .Args}}, url.PathEscape(args[{{$i}}]){{end}})
				{{- if .Query}}
				q := url.Values{}
				{{- range $qp := .Query}}
				if cmd.Flags().Changed({{quote $qp.Flag}}) {
					q.Set({{quote $qp.Name}}, fmt.Sprintf("%v", {{$qp.Var}}))
				}
				{{- end}}
				if enc := q.Encode(); enc != "" {
					path += "?" + enc
				}
				{{- end}}
				{{- if .Body}}
				body := map[string]any{}
				{{- range $f := .Body}}
				if cmd.Flags().Changed({{quote $f.Flag}}) {
					body[{{quote $f.Name}}] = {{if $f.JSON}}jsonOrString({{$f.Var}}){{else}}{{$f.Var}}{{end}}
				}
				{{- end}}
				return runAPICommand(cmd, {{quote .Method}}, path, body)
				{{- else}}
				return runAPICommand(cmd, {{quote .Method}}, path, nil)
				{{- end}}
			},
		}
		{{- range $f := .Body}}
		cmd.Flags().StringVar(&{{$f.Var}}, {{quote $f.Flag}}, "", {{quote $f.Desc}})
		{{- if $f.Required}}
		_ = cmd.MarkFlagRequired({{quote $f.Flag}})
		{{- end}}
		{{- end}}
		{{- range $qp := .Query}}
		cmd.Flags().{{$qp.FlagFunc}}(&{{$qp.Var}}, {{quote $qp.Flag}}, {{$qp.Zero}}, {{quote $qp.Desc}})
		{{- if $qp.Required}}
		_ = cmd.MarkFlagRequired({{quote $qp.Flag}})
		{{- end}}
		{{- end}}
		return cmd
	}(){{end}}
`))
