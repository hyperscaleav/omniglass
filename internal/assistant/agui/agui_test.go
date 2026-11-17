package agui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/assistant"
)

// The fixtures are narrated runs the console's conformance test validates
// against the AG-UI schemas (web/src/lib/agui.test.ts): the events this
// package writes, recorded by these tests, so the two can never disagree. A
// run that pauses on an ask, then each answer to it: approved, the call runs
// and the run finishes; declined, the call never runs and the model is told.
const (
	fixture         = "testdata/run.sse"
	fixtureApproved = "testdata/resume-approved.sse"
	fixtureDeclined = "testdata/resume-declined.sse"
)

// matchFixture holds a narration to its committed fixture, rewriting it when
// OMNIGLASS_UPDATE_FIXTURES is set.
func matchFixture(t *testing.T, path string, got []byte) {
	t.Helper()
	if os.Getenv("OMNIGLASS_UPDATE_FIXTURES") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fixture (OMNIGLASS_UPDATE_FIXTURES=1 writes it): %v", err)
	}
	if !bytes.Equal(committed, got) {
		t.Errorf("%s is stale: rerun with OMNIGLASS_UPDATE_FIXTURES=1", path)
	}
}

// events reads an SSE stream back into its events, in order.
func events(t *testing.T, stream []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(stream))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			t.Fatalf("a line that is not an SSE data line: %q", line)
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			t.Fatalf("an event that is not JSON: %v", err)
		}
		out = append(out, ev)
	}
	return out
}

func types(evs []map[string]any) []string {
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i], _ = ev["type"].(string)
	}
	return out
}

// narrate is a run the way the loop reports one: text, a read that runs, an
// ask the run pauses on.
func narrate(r *Run) {
	r.Start()
	r.Hear(assistant.Event{Kind: assistant.EventText, Text: "Let me look."})
	r.Hear(assistant.Event{Kind: assistant.EventProposed, CallID: "call-1", Tool: "list-components", Policy: "always"})
	r.Hear(assistant.Event{Kind: assistant.EventDecided, CallID: "call-1", Tool: "list-components", Decision: assistant.DecisionCalled})
	r.Hear(assistant.Event{Kind: assistant.EventResult, CallID: "call-1", Tool: "list-components", Decision: assistant.DecisionCalled, Text: `{"components":[]}`})
	r.Hear(assistant.Event{Kind: assistant.EventProposed, CallID: "call-2", Tool: "create-component", Arguments: map[string]any{"name": "lobby-display"}, Policy: "ask"})
	r.Hear(assistant.Event{Kind: assistant.EventDecided, CallID: "call-2", Tool: "create-component", Decision: assistant.DecisionWaiting})
	r.Interrupt(assistant.Pending{CallID: "call-2", Proposal: assistant.Proposal{Tool: "create-component", Title: "Create a component", Arguments: map[string]any{"name": "lobby-display"}}})
}

// A run is narrated as AG-UI events on an SSE stream: it starts, the model's
// text is one text message, each call is its start, arguments and end, a
// call that was answered is its result, and an ask ends the run with an
// interrupt naming the call it waits on and how to answer it.
func TestRunNarratesAsAGUI(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	narrate(NewRun(&buf, "lobby", "run-1"))
	evs := events(t, buf.Bytes())
	want := []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END",
		"TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END", "TOOL_CALL_RESULT",
		"TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END",
		"RUN_FINISHED",
	}
	if got := types(evs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if evs[0]["threadId"] != "lobby" || evs[0]["runId"] != "run-1" {
		t.Errorf("run started = %v, want the conversation as the thread and the run's id", evs[0])
	}
	if evs[1]["role"] != "assistant" || evs[2]["delta"] != "Let me look." || evs[2]["messageId"] != evs[1]["messageId"] || evs[3]["messageId"] != evs[1]["messageId"] {
		t.Errorf("text message = %v %v %v", evs[1], evs[2], evs[3])
	}
	if evs[4]["toolCallId"] != "call-1" || evs[4]["toolCallName"] != "list-components" || evs[5]["delta"] != "{}" {
		t.Errorf("a call with no arguments = %v %v, want its id, name and {}", evs[4], evs[5])
	}
	if evs[7]["toolCallId"] != "call-1" || evs[7]["role"] != "tool" || evs[7]["content"] != `{"components":[]}` || evs[7]["messageId"] == "" {
		t.Errorf("result = %v", evs[7])
	}
	if evs[9]["delta"] != `{"name":"lobby-display"}` {
		t.Errorf("arguments = %v, want the call's arguments as JSON", evs[9])
	}
	outcome, _ := evs[11]["outcome"].(map[string]any)
	if outcome["type"] != "interrupt" {
		t.Fatalf("finished = %v, want an interrupt outcome", evs[11])
	}
	ints, _ := outcome["interrupts"].([]any)
	if len(ints) != 1 {
		t.Fatalf("interrupts = %v, want one", outcome["interrupts"])
	}
	in, _ := ints[0].(map[string]any)
	if in["id"] != "call-2" || in["toolCallId"] != "call-2" || in["reason"] != "tool_approval" || in["message"] == "" {
		t.Errorf("interrupt = %v, want call-2 waiting on its approval", in)
	}
	schema, _ := in["responseSchema"].(map[string]any)
	if props, _ := schema["properties"].(map[string]any); props["approved"] == nil || schema["additionalProperties"] != false {
		t.Errorf("response schema = %v, want approved and nothing else", schema)
	}
	meta, _ := in["metadata"].(map[string]any)
	if meta["tool"] != "create-component" {
		t.Errorf("interrupt metadata = %v, want the proposal", meta)
	}

	matchFixture(t, fixture, buf.Bytes())
}

// A resume narrates the answered call: approved, it runs and its result is
// told before the model's reply; declined, its result says it never ran.
// Either run then finishes in success.
func TestResumeNarratesTheAnswer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path     string
		decision assistant.Decision
		isError  bool
		result   string
		reply    string
	}{
		{fixtureApproved, assistant.DecisionCalled, false, `{"name":"lobby-display"}`, "Created lobby-display."},
		{fixtureDeclined, assistant.DecisionDeclined, true, "the person declined this call; it did not run", "Understood, I left it alone."},
	} {
		var buf bytes.Buffer
		r := NewRun(&buf, "lobby", "run-2")
		r.Start()
		r.Hear(assistant.Event{Kind: assistant.EventDecided, CallID: "call-2", Tool: "create-component", Decision: tc.decision})
		r.Hear(assistant.Event{Kind: assistant.EventResult, CallID: "call-2", Tool: "create-component", Decision: tc.decision, IsError: tc.isError, Text: tc.result})
		r.Hear(assistant.Event{Kind: assistant.EventText, Text: tc.reply})
		r.Finish()
		evs := events(t, buf.Bytes())
		want := "RUN_STARTED,TOOL_CALL_RESULT,TEXT_MESSAGE_START,TEXT_MESSAGE_CONTENT,TEXT_MESSAGE_END,RUN_FINISHED"
		if got := types(evs); strings.Join(got, ",") != want {
			t.Fatalf("%s: events = %v, want %s", tc.path, got, want)
		}
		if evs[1]["toolCallId"] != "call-2" || evs[1]["content"] != tc.result {
			t.Errorf("%s: result = %v", tc.path, evs[1])
		}
		if outcome, _ := evs[5]["outcome"].(map[string]any); outcome["type"] != "success" {
			t.Errorf("%s: finished = %v, want success", tc.path, evs[5])
		}
		matchFixture(t, tc.path, buf.Bytes())
	}
}

// A run that answers without an ask finishes in success; one that fails ends
// on an error with its message and code, and nothing after it.
func TestRunFinishesOrErrs(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := NewRun(&buf, "lobby", "run-2")
	r.Start()
	r.Hear(assistant.Event{Kind: assistant.EventText, Text: "Done."})
	r.Finish()
	evs := events(t, buf.Bytes())
	last := evs[len(evs)-1]
	if outcome, _ := last["outcome"].(map[string]any); last["type"] != "RUN_FINISHED" || outcome["type"] != "success" {
		t.Errorf("finished = %v, want a success outcome", last)
	}

	buf.Reset()
	r = NewRun(&buf, "lobby", "run-3")
	r.Start()
	r.Error("the model: the recording ran out", "model")
	r.Finish()
	evs = events(t, buf.Bytes())
	if got := types(evs); strings.Join(got, ",") != "RUN_STARTED,RUN_ERROR" {
		t.Fatalf("events = %v, want the run's start and its error alone", got)
	}
	if evs[1]["message"] != "the model: the recording ran out" || evs[1]["code"] != "model" {
		t.Errorf("error = %v", evs[1])
	}
}

// A call that is declined or refused never ran, but the model is still told
// so: its result is narrated like any other.
func TestRunNarratesADeclinedResult(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := NewRun(&buf, "lobby", "run-4")
	r.Start()
	r.Hear(assistant.Event{Kind: assistant.EventDecided, CallID: "call-2", Tool: "create-component", Decision: assistant.DecisionDeclined})
	r.Hear(assistant.Event{Kind: assistant.EventResult, CallID: "call-2", Tool: "create-component", Decision: assistant.DecisionDeclined, IsError: true, Text: "the person declined this call; it did not run"})
	r.Finish()
	evs := events(t, buf.Bytes())
	if got := types(evs); strings.Join(got, ",") != "RUN_STARTED,TOOL_CALL_RESULT,RUN_FINISHED" {
		t.Fatalf("events = %v", got)
	}
	if evs[1]["toolCallId"] != "call-2" || !strings.Contains(evs[1]["content"].(string), "declined") {
		t.Errorf("result = %v", evs[1])
	}
}
