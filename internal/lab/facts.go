package lab

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Facts is what the lab guide renders (docs/src/generated/lab.json, via
// cmd/labgen on make gen): the topology the planner produces, with each
// container's privilege, and the guided walkthroughs a learner follows on the
// lab. Generated, never hand-written, so the guide cannot drift from what the
// lab runs; the drift test fails when it does.
type Facts struct {
	Published    string        `json:"published"`
	Containers   []FactService `json:"containers"`
	Volumes      []FactVolume  `json:"volumes"`
	Walkthroughs []Walkthrough `json:"walkthroughs"`
}

// FactService is one row of the topology table.
type FactService struct {
	Name     string   `json:"name"`
	Image    string   `json:"image"`
	Command  string   `json:"command"`
	User     string   `json:"user"`
	Caps     []string `json:"caps"`
	Sysctls  []string `json:"sysctls"`
	Publish  string   `json:"publish"`
	RunsOnce bool     `json:"runs_once"`
	Role     string   `json:"role"`
}

// FactVolume is one named volume the lab keeps.
type FactVolume struct {
	Name  string `json:"name"`
	Holds string `json:"holds"`
}

// Walkthrough is a guided walkthrough's id, title and description, from its
// manifest (web/e2e/walkthroughs/<id>.yaml).
type Walkthrough struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// FactsJSON renders the facts for the default lab, reading the walkthrough
// manifests from walkthroughs (the web/e2e/walkthroughs directory).
func FactsJSON(walkthroughs fs.FS) ([]byte, error) {
	spec, err := Plan(Options{Image: "omniglass", Port: DefaultPort, NodeToken: "(minted per run)"})
	if err != nil {
		return nil, err
	}
	f := Facts{Published: fmt.Sprintf("%s:%d", DefaultListen, DefaultPort)}
	for _, s := range spec.Services() {
		row := FactService{Name: s.Name, Image: s.Image, Command: strings.Join(s.Cmd, " "),
			User: s.User, Caps: append([]string{}, s.CapAdd...), RunsOnce: s.OneOff, Role: s.Role}
		if row.User == "" {
			row.User = "the image's (nonroot)"
			if s.Name == "db" {
				row.User = "the image's (postgres)"
			}
		}
		for _, k := range SortedKeys(s.Sysctls) {
			row.Sysctls = append(row.Sysctls, k+"="+s.Sysctls[k])
		}
		if s.Publish != nil {
			row.Publish = fmt.Sprintf("%s:%d -> %d", s.Publish.HostIP, s.Publish.HostPort, s.Publish.ContainerPort)
		}
		if row.Caps == nil {
			row.Caps = []string{}
		}
		if row.Sysctls == nil {
			row.Sysctls = []string{}
		}
		f.Containers = append(f.Containers, row)
	}
	for _, v := range spec.Volumes {
		f.Volumes = append(f.Volumes, FactVolume{Name: v.Name, Holds: v.Holds})
	}
	names, err := fs.Glob(walkthroughs, "*.yaml")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	for _, n := range names {
		b, err := fs.ReadFile(walkthroughs, n)
		if err != nil {
			return nil, err
		}
		var m struct {
			Title       string `yaml:"title"`
			Description string `yaml:"description"`
		}
		if err := yaml.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("walkthrough %s: %w", n, err)
		}
		f.Walkthroughs = append(f.Walkthroughs, Walkthrough{ID: strings.TrimSuffix(path.Base(n), ".yaml"), Title: m.Title, Description: m.Description})
	}
	out, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
