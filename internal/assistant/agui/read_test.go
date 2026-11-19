package agui

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/assistant"
)

// readFixture reads a committed narration back, with what it heard.
func readFixture(t *testing.T, path string, tools map[string]string) (Ending, []assistant.Event, error) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var heard []assistant.Event
	end, err := Read(f, tools, func(e assistant.Event) { heard = append(heard, e) })
	return end, heard, err
}

// Reading a narration back gives what the loop reported: each text, each
// call's result with its tool and decision, and how the run ended. The
// fixtures are the ones this package writes, so the reader and the writer
// cannot drift.
func TestReadIsTheNarrationBack(t *testing.T) {
	end, heard, err := readFixture(t, fixture, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	want := []assistant.Event{
		{Kind: assistant.EventText, Text: "Let me look."},
		{Kind: assistant.EventResult, CallID: "call-1", Tool: "list-components", Decision: assistant.DecisionCalled, Text: `{"components":[]}`},
	}
	if !reflect.DeepEqual(heard, want) {
		t.Errorf("heard %+v, want %+v", heard, want)
	}
	wantEnd := Ending{Text: "Let me look.", Waiting: &assistant.Pending{CallID: "call-2", Proposal: assistant.Proposal{
		Tool: "create-component", Title: "Create a component", Arguments: map[string]any{"name": "lobby-display"},
	}}}
	if !reflect.DeepEqual(end, wantEnd) {
		t.Errorf("ending %+v, want %+v", end, wantEnd)
	}

	// A resume run answers a call proposed in the run before: the caller
	// names its tool, and the declined text reads as a decline.
	end, heard, err = readFixture(t, fixtureDeclined, map[string]string{"call-2": "create-component"})
	if err != nil {
		t.Fatal(err)
	}
	if len(heard) != 2 || heard[0].Tool != "create-component" || heard[0].Decision != assistant.DecisionDeclined {
		t.Errorf("declined resume heard %+v, want the call declined by its tool", heard)
	}
	if end.Text != "Understood, I left it alone." || end.Waiting != nil || end.Stopped {
		t.Errorf("declined resume ended %+v, want its answer", end)
	}

	end, _, err = readFixture(t, fixtureStopped, map[string]string{})
	if err != nil || !end.Stopped || end.Steps != 10 {
		t.Errorf("step-limit run ended %+v, %v; want stopped after 10", end, err)
	}
}

// A refused call reads as refused; a run that ends in an error, or a stream
// that stops before the run ends, is an error naming why.
func TestReadEndsOnErrors(t *testing.T) {
	refused := `data: {"type":"RUN_STARTED","threadId":"t","runId":"r"}

data: {"type":"TOOL_CALL_START","toolCallId":"c","toolCallName":"drop-everything"}

data: {"type":"TOOL_CALL_RESULT","messageId":"r-1","toolCallId":"c","content":"the tool drop-everything is not offered","role":"tool"}

data: {"type":"RUN_ERROR","message":"the run stopped: the model server refused the key","code":"run"}

`
	var heard []assistant.Event
	_, err := Read(strings.NewReader(refused), map[string]string{}, func(e assistant.Event) { heard = append(heard, e) })
	if len(heard) != 1 || heard[0].Decision != assistant.DecisionRefused {
		t.Errorf("heard %+v, want the call refused", heard)
	}
	if err == nil || !strings.Contains(err.Error(), "refused the key") {
		t.Errorf("err = %v, want the run's error", err)
	}

	cut := `data: {"type":"RUN_STARTED","threadId":"t","runId":"r"}

`
	if _, err := Read(strings.NewReader(cut), map[string]string{}, func(assistant.Event) {}); err == nil || !strings.Contains(err.Error(), "before the run ended") {
		t.Errorf("a cut stream = %v, want an error saying it ended early", err)
	}
}
