package scripts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The repo has one package manager (#2705): web/ installs with pnpm, as docs/
// does, and a dependency's install script runs only when web/pnpm-workspace.yaml
// allows it by name.

// webAllowBuilds is every web/ dependency whose install script is allowed to
// run, each with the reason it needs one. Adding a package to allowBuilds means
// adding it here too.
var webAllowBuilds = map[string]string{
	"esbuild": "its postinstall fetches and checks the platform binary vite and vitest run",
}

func TestWebIsOnPnpm(t *testing.T) {
	if _, err := os.Stat("../web/pnpm-lock.yaml"); err != nil {
		t.Errorf("web/pnpm-lock.yaml is missing: %v", err)
	}
	if _, err := os.Stat("../web/package-lock.json"); err == nil {
		t.Errorf("web/package-lock.json still exists; web/ installs with pnpm")
	}
	raw, err := os.ReadFile("../web/package.json")
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		PackageManager string            `json:"packageManager"`
		Engines        map[string]string `json:"engines"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pkg.PackageManager, "pnpm@10.") {
		t.Errorf("web/package.json packageManager = %q, want pnpm@10.x", pkg.PackageManager)
	}
	if _, ok := pkg.Engines["npm"]; ok {
		t.Errorf("web/package.json engines still asks for npm")
	}
}

func TestWebAllowBuilds(t *testing.T) {
	raw, err := os.ReadFile("../web/pnpm-workspace.yaml")
	if err != nil {
		t.Fatalf("web/pnpm-workspace.yaml is missing: %v", err)
	}
	var got []string
	in := false
	for l := range strings.SplitSeq(string(raw), "\n") {
		switch {
		case strings.HasPrefix(l, "allowBuilds:"):
			in = true
		case in && strings.HasPrefix(l, "  ") && strings.Contains(l, ":"):
			k, v, _ := strings.Cut(strings.TrimSpace(l), ":")
			if strings.TrimSpace(v) == "true" {
				got = append(got, k)
			}
		case in && strings.TrimSpace(l) != "" && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "#"):
			in = false
		}
	}
	var want []string
	for k := range webAllowBuilds {
		want = append(want, k)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("web/pnpm-workspace.yaml allowBuilds = %v, want %v (record each package and why in webAllowBuilds)", got, want)
	}
}

// npmUse matches npm driving a project install or script (npm ci, npm install,
// npm run, npx). Installing a global tool (npm install -g pnpm) is not web/'s
// package manager and is allowed.
var (
	npmUse    = regexp.MustCompile(`\bnpm (ci|install|i|run|test|exec)\b|\bnpx\b`)
	npmGlobal = regexp.MustCompile(`\bnpm (install|i) (-g|--global)\b`)
)

func TestNoNpmForWeb(t *testing.T) {
	var files []string
	for _, g := range []string{
		"../.github/workflows/*.yml", "../Dockerfile", "../Makefile", "../scripts/*.sh",
		"../web/e2e/*.sh", "../web/package.json", "../CONTRIBUTING.md", "../.claude/skills/*/SKILL.md",
	} {
		m, err := filepath.Glob(g)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	// Every docs page; the build log and the decisions are history and keep
	// what was true when they were written.
	err := filepath.WalkDir("../docs/src/content/docs", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && (strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".mdx")) {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, l := range strings.Split(string(raw), "\n") {
			if npmUse.MatchString(l) && !npmGlobal.MatchString(l) {
				t.Errorf("%s:%d uses npm for a project install or script; use pnpm: %s", strings.TrimPrefix(f, "../"), i+1, strings.TrimSpace(l))
			}
		}
	}
}
