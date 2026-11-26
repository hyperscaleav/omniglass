package scripts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The repo has one package manager (#2705): web/ installs with pnpm, as docs/
// does, and a dependency's install script runs only when web/pnpm-workspace.yaml
// allows it by name. strictDepBuilds makes an install fail on any script the
// file does not decide, so a new one is a choice, never a silent skip.

// A build is one dependency install script: whether allowBuilds lets it run,
// and why.
type build struct {
	allow bool
	why   string
}

// webBuilds is every web/ dependency with an install script. A package in
// allowBuilds must be here with the same answer.
var webBuilds = map[string]build{
	"esbuild":  {true, "its postinstall fetches and checks the platform binary vite and vitest run"},
	"fsevents": {false, "macOS-only and optional; it ships its binary prebuilt, so its install script is not needed"},
}

// composeBuilds is the same for the marketing captures' camera project
// (web/e2e/marketing/compose, #2717), installed in the Playwright container on
// Linux, so only the Linux binaries' scripts are decided.
var composeBuilds = map[string]build{
	"@ffmpeg-installer/linux-x64":    {true, "its postinstall marks the ffmpeg binary executable; Revideo renders the camera pass with it"},
	"@ffmpeg-installer/linux-arm64":  {true, "the same, on an arm64 host"},
	"@ffprobe-installer/linux-x64":   {true, "its postinstall marks the ffprobe binary executable (it ships without the bit); Revideo probes the loop's frames with it"},
	"@ffprobe-installer/linux-arm64": {true, "the same, on an arm64 host"},
	"@revideo/telemetry":             {false, "it reports usage; the camera pass runs with telemetry off"},
	"puppeteer":                      {false, "it downloads a Chrome the pass does not use: the Playwright image brings its own"},
	"fsevents":                       {false, "macOS-only and optional"},
}

// pnpmProjects are the repo's pnpm projects and the install scripts each decides.
var pnpmProjects = []struct {
	dir    string
	builds map[string]build
}{
	{"web", webBuilds},
	{"web/e2e/marketing/compose", composeBuilds},
}

// packageManager reads a project's package.json pin and its engines.
func packageManager(t *testing.T, dir string) (string, map[string]string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", dir, "package.json"))
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
	return pkg.PackageManager, pkg.Engines
}

func TestWebIsOnPnpm(t *testing.T) {
	webPin, _ := packageManager(t, "web")
	for _, p := range pnpmProjects {
		if _, err := os.Stat(filepath.Join("..", p.dir, "pnpm-lock.yaml")); err != nil {
			t.Errorf("%s/pnpm-lock.yaml is missing: %v", p.dir, err)
		}
		if _, err := os.Stat(filepath.Join("..", p.dir, "package-lock.json")); err == nil {
			t.Errorf("%s/package-lock.json still exists; %s installs with pnpm", p.dir, p.dir)
		}
		pin, engines := packageManager(t, p.dir)
		if !strings.HasPrefix(pin, "pnpm@10.") {
			t.Errorf("%s/package.json packageManager = %q, want pnpm@10.x", p.dir, pin)
		}
		// One pnpm for the repo: every project pins the one web/ pins.
		if pin != webPin {
			t.Errorf("%s/package.json packageManager = %q, want web/'s %q", p.dir, pin, webPin)
		}
		if _, ok := engines["npm"]; ok {
			t.Errorf("%s/package.json engines still asks for npm", p.dir)
		}
	}
}

func TestWebAllowBuilds(t *testing.T) {
	for _, p := range pnpmProjects {
		raw, err := os.ReadFile(filepath.Join("..", p.dir, "pnpm-workspace.yaml"))
		if err != nil {
			t.Errorf("%s/pnpm-workspace.yaml is missing: %v", p.dir, err)
			continue
		}
		got := map[string]string{}
		strict := ""
		in := false
		for l := range strings.SplitSeq(string(raw), "\n") {
			switch {
			case strings.HasPrefix(l, "strictDepBuilds:"):
				_, v, _ := strings.Cut(l, ":")
				strict = strings.TrimSpace(v)
				in = false
			case strings.HasPrefix(l, "allowBuilds:"):
				in = true
			case in && strings.HasPrefix(l, "  ") && strings.Contains(l, ":"):
				k, v, _ := strings.Cut(strings.TrimSpace(l), ":")
				got[strings.Trim(k, `'"`)] = strings.TrimSpace(v)
			case in && strings.TrimSpace(l) != "" && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "#"):
				in = false
			}
		}
		if strict != "true" {
			t.Errorf("%s/pnpm-workspace.yaml strictDepBuilds = %q, want true: without it pnpm skips an undecided install script with only a warning", p.dir, strict)
		}
		want := map[string]string{}
		for k, b := range p.builds {
			want[k] = fmt.Sprint(b.allow)
		}
		keys := map[string]bool{}
		for k := range got {
			keys[k] = true
		}
		for k := range want {
			keys[k] = true
		}
		var names []string
		for k := range keys {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			if got[k] != want[k] {
				t.Errorf("%s/pnpm-workspace.yaml allowBuilds %s = %q, want %q (record each package, its answer and why in the project's builds)", p.dir, k, got[k], want[k])
			}
		}
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
		"../web/e2e/*.sh", "../web/package.json", "../CONTRIBUTING.md",
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
