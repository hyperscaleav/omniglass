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
	groups := group(cmds)

	out, err := render(groups)
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
	Name string `json:"name"`
	In   string `json:"in"`
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
	Description string `json:"description"`
}

var httpMethods = []string{"get", "post", "put", "patch", "delete"}

// --- command model ----------------------------------------------------------

type command struct {
	Words   []string // command path, e.g. ["location", "get"] or ["healthz"]
	Method  string   // GET, POST, ...
	APIPath string   // Go format template, e.g. /api/v1/locations/%s
	Args    []string // positional arg names, in path order
	Body    []bodyField
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
}

// nameOverride maps the non-AIP utility routes (no resource collection) to their
// command words; everything else derives from the path. This is the documented
// seam for naming an operation the heuristic cannot.
var nameOverride = map[string]([]string){
	"get-healthz":             {"healthz"},
	"get-auth-me":             {"auth", "me"},
	"update-auth-me":          {"auth", "update-profile"},
	"change-auth-me-password": {"auth", "change-password"},
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

	// Path params become positional args and %s slots, in path order.
	apiPath := base
	var args []string
	for _, seg := range splitPath(path) {
		if name, ok := pathParam(seg); ok {
			apiPath += "/%s"
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
	c.Example = example(words, args, c.Body)
	return c
}

// commandWords derives the cobra command path. An override wins; otherwise the
// AIP path drives it: a `{id}:verb` custom method is `<resource> <verb>`, an
// item path is `<resource> <get|update|delete>`, and a collection is
// `<resource> <list|create>`.
func commandWords(path, method, opID string) []string {
	if w, ok := nameOverride[opID]; ok {
		return w
	}
	segs := splitPath(path)
	last := segs[len(segs)-1]

	// Custom method: the final segment is {id}:verb (or collection:verb).
	if i := strings.Index(last, ":"); i >= 0 {
		verb := last[i+1:]
		container := last[:i]
		if name, ok := pathParam(container); ok {
			_ = name
			// resource is the collection segment before the {id}
			return []string{singular(segs[len(segs)-2]), verb}
		}
		return []string{singular(container), verb}
	}

	// Item operation: collection/{id}.
	if _, ok := pathParam(last); ok {
		resource := singular(segs[len(segs)-2])
		switch method {
		case "get":
			return []string{resource, "get"}
		case "patch", "put":
			return []string{resource, "update"}
		case "delete":
			return []string{resource, "delete"}
		}
	}

	// Collection operation: list or create.
	resource := singular(last)
	switch method {
	case "post":
		return []string{resource, "create"}
	default:
		return []string{resource, "list"}
	}
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
		fields = append(fields, bodyField{
			Name:     k,
			Flag:     strings.ReplaceAll(k, "_", "-"),
			Var:      "f" + goIdent(k),
			Desc:     sc.Properties[k].Description,
			Required: required[k],
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

type cmdGroup struct {
	Parent   string    // "" for a standalone command
	Children []command // verbs under the parent, or one standalone command
}

func group(cmds []command) []cmdGroup {
	order := []string{}
	byParent := map[string][]command{}
	for _, c := range cmds {
		parent := ""
		if len(c.Words) > 1 {
			parent = c.Words[0]
		}
		if _, seen := byParent[parent]; !seen {
			order = append(order, parent)
		}
		byParent[parent] = append(byParent[parent], c)
	}
	sort.Strings(order)
	var groups []cmdGroup
	for _, parent := range order {
		kids := byParent[parent]
		sort.Slice(kids, func(i, j int) bool {
			return strings.Join(kids[i].Words, " ") < strings.Join(kids[j].Words, " ")
		})
		groups = append(groups, cmdGroup{Parent: parent, Children: kids})
	}
	return groups
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

// singular is a naive depluralizer good enough for the AIP collection nouns in
// use (locations, roles). A real irregular set can join nameOverride if needed.
func singular(s string) string {
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

func render(groups []cmdGroup) ([]byte, error) {
	var b strings.Builder
	if err := tmpl.Execute(&b, groups); err != nil {
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
{{range $g := .}}
{{- if eq $g.Parent ""}}
	{{- range $c := $g.Children}}
	roots = append(roots, {{template "leaf" $c}})
	{{- end}}
{{- else}}
	{
		parent := &cobra.Command{
			Use:   {{quote $g.Parent}},
			Short: {{quote (printf "Commands for the %s resource" $g.Parent)}},
		}
		{{- range $c := $g.Children}}
		parent.AddCommand({{template "leaf" $c}})
		{{- end}}
		roots = append(roots, parent)
	}
{{- end}}
{{- end}}
	return roots
}

{{define "leaf"}}func() *cobra.Command {
		{{- range $f := .Body}}
		var {{$f.Var}} string
		{{- end}}
		cmd := &cobra.Command{
			Use:     {{quote .LeafUse}},
			Short:   {{quote .Short}},
			Long:    {{quote .Long}},
			Example: {{quote .Example}},
			Args:    cobra.ExactArgs({{len .Args}}),
			RunE: func(cmd *cobra.Command, args []string) error {
				path := fmt.Sprintf({{quote .APIPath}}{{range $i, $a := .Args}}, url.PathEscape(args[{{$i}}]){{end}})
				{{- if .Body}}
				body := map[string]any{}
				{{- range $f := .Body}}
				if cmd.Flags().Changed({{quote $f.Flag}}) {
					body[{{quote $f.Name}}] = {{$f.Var}}
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
		return cmd
	}(){{end}}
`))
