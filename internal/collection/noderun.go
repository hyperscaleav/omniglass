package collection

import (
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The on-demand node run (#1163, ADR-1163): the server asks a node to run
// work once, now, and answers its caller with what the run read. One
// primitive, two outcomes: a preview runs polls of a spec no endpoint holds
// yet and stores nothing, and a run now runs an attached endpoint's poll
// outside its schedule and lands its samples as a scheduled run does. It is a
// request the server makes and the node answers once (the simulator
// operation's shape), not the command wire's pull: a run is wanted now or not
// at all, so a node that is not there is an answer, never a queued run.

// RunSubject is where the server asks a node to run work now (request-reply,
// the server requesting). The node subscribes with its own grant and answers
// each request once.
func RunSubject(node string) string { return subjectPrefix + "run." + node }

// The answers a run gets when the node does not run it.
var (
	// ErrNodeAbsent: the node is not connected (no one listens on its run
	// subject).
	ErrNodeAbsent = errors.New("the node is not connected")
	// ErrNodeTimeout: the node did not answer within the run's budget.
	ErrNodeTimeout = errors.New("the node did not answer in time")
	// ErrTaskNotOnWorklist: the task is not on the node's worklist (disabled,
	// or its endpoint moved since it was resolved).
	ErrTaskNotOnWorklist = errors.New("the task is not on the node's worklist")
)

// The two outcomes of a run.
const (
	// RunPreview runs the tasks and answers with what they read; nothing is
	// published, so nothing is stored.
	RunPreview = "preview"
	// RunNow runs an attached endpoint's poll task as a scheduled run would,
	// landing its samples, and answers with what it read.
	RunNow = "run"
)

// RunRequest is one run asked of a node: its outcome, the tasks to run (each
// as a worklist delivers it, its secret inputs unsealed), and how long the
// whole run may take.
type RunRequest struct {
	Kind      string     `json:"kind"`
	Tasks     []TaskSpec `json:"tasks"`
	TimeoutMs int64      `json:"timeout_ms,omitempty"`
}

// RunSample is one sample as the run wire carries it: a metric's number, a
// property's text, an event's or a log line's lane and message.
type RunSample struct {
	Name   string            `json:"name" doc:"The key the emit landed"`
	Lane   string            `json:"lane,omitempty" doc:"event or log for an event or a log line; absent for a metric or a property"`
	Value  *float64          `json:"value,omitempty" doc:"A metric's number"`
	Text   string            `json:"text,omitempty" doc:"A property's value, or an event's or log line's message"`
	Labels map[string]string `json:"labels,omitempty" doc:"The sample's labels, its instance among them"`
	TS     time.Time         `json:"ts" doc:"When the sample was taken"`
}

// RunSampleOf maps a collected sample onto the run wire.
func RunSampleOf(s Sample) RunSample {
	out := RunSample{Name: s.Name, Lane: s.Lane, Labels: s.Labels, TS: s.TS}
	if s.IsText || s.Lane != "" {
		out.Text = s.Text
	} else {
		v := s.Value
		out.Value = &v
	}
	return out
}

// RunResult is what one task's run read: its samples, the faults it met (each
// an emit that did not locate or type, or the fetch that failed), and an
// excerpt of the answer its fetch got, before any emit read it.
type RunResult struct {
	TaskID           string      `json:"task_id"`
	Function         string      `json:"function"`
	Samples          []RunSample `json:"samples"`
	Faults           []string    `json:"faults,omitempty"`
	Excerpt          string      `json:"excerpt,omitempty"`
	ExcerptTruncated bool        `json:"excerpt_truncated,omitempty"`
	TookMs           int64       `json:"took_ms"`
}

// RunReply is the node's answer to a run: a result per task, or, when the run
// could not start, why (Gated when the endpoint's gate holds and so refused
// the run, the gate's refusal in Error).
type RunReply struct {
	Results []RunResult `json:"results,omitempty"`
	Error   string      `json:"error,omitempty"`
	Gated   bool        `json:"gated,omitempty"`
}

// ExcerptLimit bounds a run's excerpt of an answer.
const ExcerptLimit = 4096

// PayloadExcerpt renders what a fetch answered, bounded to max bytes and cut
// on a rune boundary: an snmp answer as one line per oid in order, any other
// as its text. cut reports that the answer was longer.
func PayloadExcerpt(p driver.Payload, max int) (excerpt string, cut bool) {
	var s string
	if len(p.Values) > 0 {
		oids := make([]string, 0, len(p.Values))
		for oid := range p.Values {
			oids = append(oids, oid)
		}
		sort.Strings(oids)
		lines := make([]string, 0, len(oids))
		for _, oid := range oids {
			lines = append(lines, oid+" = "+p.Values[oid])
		}
		s = strings.Join(lines, "\n")
	} else {
		s = p.Text
		if s == "" && len(p.Body) > 0 {
			s = string(p.Body)
		}
	}
	if len(s) <= max {
		return s, false
	}
	cutAt := max
	for cutAt > 0 && !utf8.RuneStart(s[cutAt]) {
		cutAt--
	}
	return s[:cutAt], true
}
