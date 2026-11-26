package scripts

import (
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// The docs-shots recapture runs on a push to main or a release branch that
// touches an input to a capture (#860): the dev seed fleet the captured console
// renders, the boot seed, the migrations behind it, and what builds the lab
// image. Without these a seed-only change merged to main would never be
// recaptured until a later docs/** or web/** push. The pull_request filter carries
// the same inputs, so a seed-only PR runs the verify job.
func TestDocsShotsRecaptureTriggersOnCaptureInputs(t *testing.T) {
	b, err := os.ReadFile("../.github/workflows/docs-shots.yml")
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		On struct {
			Push struct {
				Paths []string `yaml:"paths"`
			} `yaml:"push"`
			PullRequest struct {
				Paths []string `yaml:"paths"`
			} `yaml:"pull_request"`
		} `yaml:"on"`
	}
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"docs/**", "web/**", // the pages and the console
		"internal/devseed/**", "internal/seed/**", "db/migrations/**", // the fleet and the rows behind it
		"Makefile", "Dockerfile*", ".github/workflows/docs-shots.yml", // what builds the lab image, and this workflow
	} {
		if !slices.Contains(wf.On.Push.Paths, want) {
			t.Errorf("docs-shots.yml push.paths lacks %q (has %v): a change there would not be recaptured on main", want, wf.On.Push.Paths)
		}
	}
	// A PR runs the structural verify job (#2561): cheap, and it starts on a seed-only
	// change too, so the filter carries every input the push filter does (#860).
	for _, want := range wf.On.Push.Paths {
		if !slices.Contains(wf.On.PullRequest.Paths, want) {
			t.Errorf("docs-shots.yml pull_request.paths lacks %q (has %v): a PR touching only that input would not run docs-shots", want, wf.On.PullRequest.Paths)
		}
	}
}
