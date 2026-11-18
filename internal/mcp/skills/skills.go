// Package skills is the registry of omniglass's agent skills (#2026): opinionated
// workflows (triage an alarm, troubleshoot a component) that tell an agent the
// house way to do a job with the MCP tools. Each skill is one directory holding
// one SKILL.md, in the Agent Skills format (YAML front matter, then the Markdown
// workflow), embedded in the binary. Adding a skill is adding a directory: the
// MCP server serves every one as a prompt, `omniglass mcp skill` prints it, and
// `make gen` renders the docs page from the same bytes.
//
// A skill names the tools it uses in its body as `tool:<name>` references, and
// nothing else: the tool set is parsed from the body, and the permissions a
// skill needs are the gates of those tools in the catalog, so neither is
// restated here. A test in internal/mcp fails any reference the catalog lacks.
package skills

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed */SKILL.md
var embedded embed.FS

// Categories are the workflow groups a skill files under, in the order the
// docs list them.
var Categories = []string{"operate", "build", "administer", "report"}

// Argument is one MCP prompt argument a skill takes.
type Argument struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

// Skill is one loaded SKILL.md.
type Skill struct {
	Name        string
	Description string
	Category    string
	Arguments   []Argument
	// Body is the Markdown workflow after the front matter.
	Body string
	// Raw is the file's bytes, front matter included, as an Agent Skills loader
	// reads it.
	Raw []byte
	// Tools are the tools the body references, each once, in first-mention order.
	Tools []string
}

type frontMatter struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Category    string     `yaml:"category"`
	Arguments   []Argument `yaml:"arguments"`
}

// toolRef is a tool reference in a skill body: `tool:<name>`.
var toolRef = regexp.MustCompile("`tool:([a-z0-9-]+)`")

// argName is the shape of an argument name.
var argName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

const fence = "---\n"

// Load reads every <name>/SKILL.md in fsys, validated and sorted by name.
func Load(fsys fs.FS) ([]Skill, error) {
	paths, err := fs.Glob(fsys, "*/SKILL.md")
	if err != nil {
		return nil, err
	}
	var out []Skill
	for _, p := range paths {
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		s, err := parse(path.Dir(p), raw)
		if err != nil {
			return nil, fmt.Errorf("skill %s: %w", path.Dir(p), err)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func parse(dir string, raw []byte) (Skill, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(text, fence) {
		return Skill{}, errors.New("no front matter: the file must open with a --- line")
	}
	head, body, ok := strings.Cut(text[len(fence):], "\n"+fence)
	if !ok {
		return Skill{}, errors.New("the front matter is not closed with a --- line")
	}
	var fm frontMatter
	dec := yaml.NewDecoder(bytes.NewReader([]byte(head)))
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil {
		return Skill{}, fmt.Errorf("front matter: %w", err)
	}
	switch {
	case fm.Name != dir:
		return Skill{}, fmt.Errorf("name %q differs from its directory", fm.Name)
	case strings.TrimSpace(fm.Description) == "":
		return Skill{}, errors.New("no description")
	case !slices.Contains(Categories, fm.Category):
		return Skill{}, fmt.Errorf("category %q is not one of %s", fm.Category, strings.Join(Categories, ", "))
	}
	seen := map[string]bool{}
	for _, a := range fm.Arguments {
		if !argName.MatchString(a.Name) {
			return Skill{}, fmt.Errorf("argument name %q is not lower_snake_case", a.Name)
		}
		if seen[a.Name] {
			return Skill{}, fmt.Errorf("argument %q is declared twice", a.Name)
		}
		seen[a.Name] = true
	}
	body = strings.TrimLeft(body, "\n")
	var tools []string
	for _, m := range toolRef.FindAllStringSubmatch(body, -1) {
		if !slices.Contains(tools, m[1]) {
			tools = append(tools, m[1])
		}
	}
	if len(tools) == 0 {
		return Skill{}, errors.New("the body names no tool (`tool:<name>`)")
	}
	return Skill{
		Name: fm.Name, Description: strings.TrimSpace(fm.Description), Category: fm.Category,
		Arguments: fm.Arguments, Body: body, Raw: raw, Tools: tools,
	}, nil
}

// Prompt is the skill as an MCP prompt hands it to an agent: the body, then the
// arguments the client gave, in declared order. A missing required argument or
// an undeclared one is refused.
func (s Skill) Prompt(args map[string]string) (string, error) {
	for name := range args {
		if !slices.ContainsFunc(s.Arguments, func(a Argument) bool { return a.Name == name }) {
			return "", fmt.Errorf("skill %s takes no argument %q", s.Name, name)
		}
	}
	var given []string
	for _, a := range s.Arguments {
		v := strings.TrimSpace(args[a.Name])
		if v == "" {
			if a.Required {
				return "", fmt.Errorf("skill %s needs the argument %q", s.Name, a.Name)
			}
			continue
		}
		given = append(given, "- "+a.Name+": "+v)
	}
	if len(given) == 0 {
		return s.Body, nil
	}
	return strings.TrimRight(s.Body, "\n") + "\n\n## Given\n\n" + strings.Join(given, "\n") + "\n", nil
}

var (
	loadOnce sync.Once
	loaded   []Skill
)

// All is every embedded skill, sorted by name. A malformed embedded skill is a
// build defect, caught by this package's tests, so it panics here.
func All() []Skill {
	loadOnce.Do(func() {
		var err error
		if loaded, err = Load(embedded); err != nil {
			panic(err)
		}
	})
	return loaded
}

// Get is the embedded skill named name.
func Get(name string) (Skill, bool) {
	for _, s := range All() {
		if s.Name == name {
			return s, true
		}
	}
	return Skill{}, false
}
