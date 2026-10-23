package storage

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/hyperscaleav/omniglass/internal/expression"
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
	judges map[ruleJudgeKey]compiled
	logged map[ruleJudgeKey]bool
}

// compiled is one rule version's compile: its judge, or the error that
// stopped it. A failure is held like a success, so a rule whose criteria
// cannot compile (a data type changed under them, a row the backfill left
// in short form) costs one compile per version, not one per sample.
type compiled struct {
	judge *rules.Judge
	err   error
}

type ruleJudgeKey struct {
	id       string
	version  int
	dataType string
}

const ruleJudgeCap = 4096

func judgeKey(r EventRule) ruleJudgeKey { return ruleJudgeKey{r.ID, r.Version, r.dataType} }

// judge is the rule's compiled criteria, compiled on its version's first
// use; a compile error is cached and returned the same way.
func (j *ruleJudges) judge(r EventRule) (*rules.Judge, error) {
	k := judgeKey(r)
	j.mu.Lock()
	if c, ok := j.judges[k]; ok {
		j.mu.Unlock()
		return c.judge, c.err
	}
	j.mu.Unlock()
	jj, err := rules.Compile(r.SampleLane, r.dataType, r.FireCriteria, r.ClearCriteria)
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.judges == nil || len(j.judges) >= ruleJudgeCap {
		j.judges = map[ruleJudgeKey]compiled{}
	}
	if have, ok := j.judges[k]; ok {
		return have.judge, have.err
	}
	j.judges[k] = compiled{judge: jj, err: err}
	return jj, err
}

// withCriteriaNotes sets r's CriteriaError and CriteriaWarnings from the
// same cached compile the evaluator judges with: why its stored criteria do
// not compile, or the warnings they compile with.
func (p *PG) withCriteriaNotes(r *EventRule) {
	j, err := p.judges.judge(*r)
	r.CriteriaError, r.CriteriaWarnings = nil, nil
	if err != nil {
		r.CriteriaError = criteriaFault(err)
		return
	}
	for _, w := range j.Warnings() {
		r.CriteriaWarnings = append(r.CriteriaWarnings, CriteriaFault{Field: w.Field, Line: w.Line, Column: w.Column, Message: w.Message, Code: w.Code})
	}
}

// criteriaFault is a criteria compile error as a reader sees it.
func criteriaFault(err error) *CriteriaFault {
	f := &CriteriaFault{Message: err.Error()}
	var ce *rules.CriteriaError
	if errors.As(err, &ce) {
		f.Field, f.Message = ce.Field, ce.Err.Error()
	}
	var ee *expression.Error
	if errors.As(err, &ee) {
		f.Line, f.Column, f.Message, f.Code = ee.Line, ee.Column, ee.Message, ee.Code
	}
	return f
}

// firstFault reports whether this is the first fault logged for the rule's
// version in this process, so a rule that cannot judge its samples (a run
// over its budget, a value its lane cannot read, criteria that do not
// compile) logs once per rule version per process, not once per sample.
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
			slog.Warn("event rule cannot evaluate a sample; it holds (logged once per rule version per process)", "facility", "rules",
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
