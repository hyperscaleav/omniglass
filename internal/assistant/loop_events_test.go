package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
)

// recorder keeps a run's events as one line each, so a test reads their order.
type recorder struct{ lines []string }

func (r *recorder) hear(e Event) {
	switch e.Kind {
	case EventText:
		r.lines = append(r.lines, "text "+e.Text)
	case EventProposed:
		args, _ := json.Marshal(e.Arguments)
		r.lines = append(r.lines, fmt.Sprintf("proposed %s %s %s %s", e.CallID, e.Tool, e.Policy, args))
	case EventDecided:
		r.lines = append(r.lines, fmt.Sprintf("decided %s %s", e.CallID, e.Decision))
	case EventResult:
		r.lines = append(r.lines, fmt.Sprintf("result %s %s error=%t", e.CallID, e.Text, e.IsError))
	}
}

const readThenAsk = `
turns:
  - text: Looking first.
    tool_calls: [{name: list-components}, {name: create-component, arguments: {name: x}}]
  - text: Done.
`

// Every step reaches the hook as it happens: the turn's text, then each call
// by its provider id and arguments, its decision, and its result.
func TestLoopEventsNameEachCallInOrder(t *testing.T) {
	t.Parallel()
	var asked []Proposal
	var rec recorder
	l := &Loop{Provider: replay(t, readThenAsk), Tools: fleet(), Confirm: confirmer(true, &asked), Events: rec.hear}
	if _, err := l.Run(context.Background(), "add x"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"text Looking first.",
		"proposed call-1 list-components always null",
		"decided call-1 called",
		"result call-1 list-components done error=false",
		`proposed call-2 create-component ask {"name":"x"}`,
		"decided call-2 called",
		"result call-2 create-component done error=false",
		"text Done.",
	}
	if !reflect.DeepEqual(rec.lines, want) {
		t.Errorf("events:\n%s\nwant:\n%s", strings.Join(rec.lines, "\n"), strings.Join(want, "\n"))
	}
}

// A refused call is decided and answered without being put to anyone.
func TestLoopEventsRefusedCall(t *testing.T) {
	t.Parallel()
	var rec recorder
	l := &Loop{Provider: replay(t, "turns:\n  - tool_calls: [{name: delete-component}]\n  - text: ok\n"), Tools: fleet(), Events: rec.hear}
	if _, err := l.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"proposed call-1 delete-component  null",
		"decided call-1 refused",
		"result call-1 the tool delete-component is not offered error=true",
		"text ok",
	}
	if !reflect.DeepEqual(rec.lines, want) {
		t.Errorf("events:\n%s\nwant:\n%s", strings.Join(rec.lines, "\n"), strings.Join(want, "\n"))
	}
}

// A pausing loop stops at the ask rather than asking: the outcome names the
// pending call and carries the conversation, and nothing past the read ran.
func TestLoopPausesAtAnAsk(t *testing.T) {
	t.Parallel()
	cat := fleet()
	var rec recorder
	l := &Loop{Provider: replay(t, readThenAsk), Tools: cat, Pause: true, Events: rec.hear}
	out, err := l.Run(context.Background(), "add x")
	if err != nil {
		t.Fatal(err)
	}
	p := out.Pending
	if p == nil || p.CallID != "call-2" || p.Tool != "create-component" || p.Title != "Do create-component" || p.Arguments["name"] != "x" || !p.Destructive {
		t.Fatalf("pending = %+v, want create-component call-2", p)
	}
	if strings.Join(cat.called, ",") != "list-components" {
		t.Errorf("called = %v, want the read alone", cat.called)
	}
	if last := rec.lines[len(rec.lines)-1]; last != "decided call-2 waiting" {
		t.Errorf("last event = %q, want the call waiting", last)
	}
	if n := len(out.Messages); n != 3 || out.Messages[2].ToolCallID != "call-1" {
		t.Errorf("messages = %+v, want the prompt, the turn and the read's result", out.Messages)
	}
	if out.Stopped || out.Text != "" {
		t.Errorf("outcome = %+v, want paused, not finished", out)
	}
}

// Continued with an approval, a paused run ends in the same transcript an
// inline approval produces.
func TestLoopContinueApprovedMatchesInline(t *testing.T) {
	t.Parallel()
	var asked []Proposal
	inline, err := (&Loop{Provider: replay(t, readThenAsk), Tools: fleet(), Confirm: confirmer(true, &asked)}).Run(context.Background(), "add x")
	if err != nil {
		t.Fatal(err)
	}

	cat := fleet()
	var rec recorder
	l := &Loop{Provider: replay(t, readThenAsk), Tools: cat, Pause: true, Events: rec.hear}
	paused, err := l.Run(context.Background(), "add x")
	if err != nil {
		t.Fatal(err)
	}
	rec.lines = nil
	out, err := l.Continue(context.Background(), paused.Messages, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Messages, inline.Messages) {
		t.Errorf("continued transcript:\n%+v\ninline:\n%+v", out.Messages, inline.Messages)
	}
	if out.Text != "Done." || out.Pending != nil || out.Steps != 2 {
		t.Errorf("outcome = %+v", out)
	}
	if strings.Join(cat.called, ",") != "list-components,create-component" {
		t.Errorf("called = %v", cat.called)
	}
	want := []string{"decided call-2 called", "result call-2 create-component done error=false", "text Done."}
	if !reflect.DeepEqual(rec.lines, want) {
		t.Errorf("continue events = %v, want %v", rec.lines, want)
	}
}

// Continued with a decline, the call does not run and the model is told so.
func TestLoopContinueDeclinedWritesNothing(t *testing.T) {
	t.Parallel()
	cat := fleet()
	l := &Loop{Provider: replay(t, readThenAsk), Tools: cat, Pause: true}
	paused, err := l.Run(context.Background(), "add x")
	if err != nil {
		t.Fatal(err)
	}
	out, err := l.Continue(context.Background(), paused.Messages, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cat.called, ",") != "list-components" {
		t.Errorf("called = %v, want the declined write not run", cat.called)
	}
	if got := decisions(out); got != "create-component:declined" {
		t.Errorf("decisions = %s", got)
	}
	res := out.Messages[3]
	if res.Role != provider.RoleTool || res.ToolCallID != "call-2" || !res.IsError || !strings.Contains(res.Text, "declined") {
		t.Errorf("the model was told %+v, want the call declined", res)
	}
}

// A pause before the last call of a turn leaves the rest of the turn for
// Continue, which answers them in order and can pause again.
func TestLoopContinueFinishesTheTurnInOrder(t *testing.T) {
	t.Parallel()
	cat := fleet()
	l := &Loop{Provider: replay(t, `
turns:
  - tool_calls: [{name: create-component, arguments: {name: a}}, {name: list-components}, {name: unmarked}]
  - text: Done.
`), Tools: cat, Pause: true}
	first, err := l.Run(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if first.Pending == nil || first.Pending.CallID != "call-1" || len(cat.called) != 0 {
		t.Fatalf("first pause = %+v, called %v", first.Pending, cat.called)
	}
	second, err := l.Continue(context.Background(), first.Messages, true)
	if err != nil {
		t.Fatal(err)
	}
	if second.Pending == nil || second.Pending.CallID != "call-3" || strings.Join(cat.called, ",") != "create-component,list-components" {
		t.Fatalf("second pause = %+v, called %v", second.Pending, cat.called)
	}
	done, err := l.Continue(context.Background(), second.Messages, true)
	if err != nil {
		t.Fatal(err)
	}
	if done.Text != "Done." || strings.Join(cat.called, ",") != "create-component,list-components,unmarked" {
		t.Errorf("done = %+v, called %v", done, cat.called)
	}
}

// Continue on a conversation with no call waiting is an error, not a new turn.
func TestLoopContinueWithNothingWaiting(t *testing.T) {
	t.Parallel()
	l := &Loop{Provider: replay(t, "turns:\n  - text: hi\n"), Tools: fleet()}
	out, err := l.Run(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Continue(context.Background(), out.Messages, true); err == nil {
		t.Error("continued a finished conversation")
	}
	if _, err := l.Continue(context.Background(), nil, true); err == nil {
		t.Error("continued an empty conversation")
	}
}

// Continue does not write into the caller's history.
func TestLoopContinueLeavesHistoryAlone(t *testing.T) {
	t.Parallel()
	l := &Loop{Provider: replay(t, readThenAsk), Tools: fleet(), Pause: true}
	paused, err := l.Run(context.Background(), "add x")
	if err != nil {
		t.Fatal(err)
	}
	held := append([]provider.Message(nil), paused.Messages...)
	history := make([]provider.Message, len(held), len(held)+8)
	copy(history, held)
	if _, err := l.Continue(context.Background(), history, true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(history, held) || len(history[:cap(history)][len(held)].Text) != 0 {
		t.Error("Continue changed the history it was given")
	}
}

// A call's decision is heard before the call is made, so a client can show it
// running; its result after.
func TestLoopEventsDecideBeforeTheCallRuns(t *testing.T) {
	t.Parallel()
	cat := fleet()
	var seen []string
	var asked []Proposal
	l := &Loop{Provider: replay(t, readThenAsk), Tools: cat, Confirm: confirmer(true, &asked),
		Events: func(e Event) {
			if e.Kind == EventDecided || e.Kind == EventResult {
				seen = append(seen, fmt.Sprintf("%s %s ran=%d", e.Kind, e.Tool, len(cat.called)))
			}
		}}
	if _, err := l.Run(context.Background(), "add x"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"decided list-components ran=0", "result list-components ran=1",
		"decided create-component ran=1", "result create-component ran=2",
	}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("seen = %v, want %v", seen, want)
	}
}
