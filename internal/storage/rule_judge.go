package storage

import (
	"log/slog"
	"sync"

	"github.com/hyperscaleav/omniglass/internal/rules"
)

// ruleJudges holds each rule's compiled criteria per (rule, version, data
// type) (#957), so every path that decides a sample (the ingest, the prime,
// the sweep, the reconcile scan and its settle) evaluates a program compiled
// once per rule version, never per sample or per batch. A rule write bumps
// the version, so an edit is a new key and needs no invalidation; the data
// type is in the key because it types the value the program reads. Bounded:
// past ruleJudgeCap entries the map starts over (keys are content-addressed,
// a recompile is cheap). The zero value is ready; safe for concurrent use.
type ruleJudges struct {
	mu     sync.Mutex
	judges map[ruleJudgeKey]*rules.Judge
	logged map[ruleJudgeKey]bool
}

type ruleJudgeKey struct {
	id       string
	version  int
	dataType string
}

const ruleJudgeCap = 4096

func judgeKey(r EventRule) ruleJudgeKey { return ruleJudgeKey{r.ID, r.Version, r.dataType} }

// judge is the rule's compiled criteria, compiled on its version's first use.
func (j *ruleJudges) judge(r EventRule) (*rules.Judge, error) {
	k := judgeKey(r)
	j.mu.Lock()
	if jj, ok := j.judges[k]; ok {
		j.mu.Unlock()
		return jj, nil
	}
	j.mu.Unlock()
	jj, err := rules.Compile(r.SampleLane, r.dataType, r.FireCriteria, r.ClearCriteria)
	if err != nil {
		return nil, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.judges == nil || len(j.judges) >= ruleJudgeCap {
		j.judges = map[ruleJudgeKey]*rules.Judge{}
	}
	if have, ok := j.judges[k]; ok {
		return have, nil
	}
	j.judges[k] = jj
	return jj, nil
}

// firstFault reports whether this is the first fault logged for the rule's
// version, so a rule that cannot judge its samples (a run over its budget, a
// value its lane cannot read) logs once, not once per sample.
func (j *ruleJudges) firstFault(r EventRule) bool {
	k := judgeKey(r)
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.logged == nil || len(j.logged) >= ruleJudgeCap {
		j.logged = map[ruleJudgeKey]bool{}
	}
	if j.logged[k] {
		return false
	}
	j.logged[k] = true
	return true
}

// decide is what one sample means for a rule on its owner, by the rule's
// compiled criteria. An error (the rule cannot judge the sample) is logged
// once per rule version and decides Hold, which breaks any streak the
// series held; the sample itself always lands.
func (p *PG) decide(r EventRule, o *ruleOwner, instance string, value any) (rules.Action, bool) {
	s := rules.Sample{Value: value, Key: r.SampleKey, Instance: instance,
		Component: rules.Component{Name: o.comp.Name, Type: o.typeName}}
	action, err := p.decideSample(r, s)
	if err != nil {
		if p.judges.firstFault(r) {
			slog.Warn("event rule cannot evaluate a sample; it holds (logged once per rule version)", "facility", "rules",
				"rule", r.Name, "version", r.Version, "component", o.comp.Name, "key", r.SampleKey, "error", err.Error())
		}
		return rules.Hold, false
	}
	return action, true
}

func (p *PG) decideSample(r EventRule, s rules.Sample) (rules.Action, error) {
	j, err := p.judges.judge(r)
	if err != nil {
		return rules.Hold, err
	}
	return j.Decide(s)
}
