package seed

import (
	"fmt"
	"sort"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// factsPlatformFigure is one figure of the Platform health page and who
// watches it (#1983): the event rules whose sample_key it is, or the reason it
// has none. Rendered on the collection page, so an operator sees which figures
// alarm and which do not.
type factsPlatformFigure struct {
	Key       string   `json:"key"`
	Group     string   `json:"group"`
	Rules     []string `json:"rules,omitempty"`
	Exemption string   `json:"exemption,omitempty"`
}

// platformCoverage joins the page's health figures to the rules watching them
// (ruleKeys maps a sample key to the rules on it) and to their exemptions, one
// row per figure in page order, and returns what is wrong with the join: a
// figure with neither a rule nor an exemption, an exemption with no reason,
// an exemption a rule has overtaken, an exemption naming no figure. Pure, so
// the gate's own behavior is tested without the real lists.
func platformCoverage(figures []storage.PlatformHealthKey, ruleKeys map[string][]string, exempt map[string]string) (rows []factsPlatformFigure, problems []string) {
	isFigure := map[string]bool{}
	for _, f := range figures {
		isFigure[f.Key] = true
		row := factsPlatformFigure{Key: f.Key, Group: f.Group, Rules: ruleKeys[f.Key]}
		reason, exempted := exempt[f.Key]
		switch {
		case len(row.Rules) == 0 && !exempted:
			problems = append(problems, fmt.Sprintf("%s has neither a shipped event rule nor an entry in PlatformRuleExemptions", f.Key))
		case len(row.Rules) > 0 && exempted:
			problems = append(problems, fmt.Sprintf("%s has a rule and an exemption: delete the exemption", f.Key))
		case exempted && reason == "":
			problems = append(problems, fmt.Sprintf("%s has an exemption with no reason", f.Key))
		}
		row.Exemption = reason
		rows = append(rows, row)
	}
	var stale []string
	for key := range exempt {
		if !isFigure[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		problems = append(problems, fmt.Sprintf("the exemption for %s names it, but %s is not a platform health figure", key, key))
	}
	return rows, problems
}
