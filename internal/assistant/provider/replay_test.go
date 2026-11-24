package provider

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

const recording = `
turns:
  - tool_calls:
      - name: list-components
      - name: create-component
        arguments: {name: lobby-display, product: generic-device}
  - text: Created lobby-display.
`

// A replay plays its recorded turns in order, numbering every call, whatever
// the conversation so far, and says so when the recording runs out.
func TestReplayPlaysTurnsInOrder(t *testing.T) {
	t.Parallel()
	r, err := ParseReplay([]byte(recording))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := r.Next(ctx, []Message{{Role: RoleUser, Text: "add a display"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 2 || first.Text != "" {
		t.Fatalf("first turn = %+v, want two calls", first)
	}
	if c := first.ToolCalls[0]; c.ID != "call-1" || c.Name != "list-components" || len(c.Arguments) != 0 {
		t.Errorf("first call = %+v", c)
	}
	if c := first.ToolCalls[1]; c.ID != "call-2" || c.Arguments["name"] != "lobby-display" {
		t.Errorf("second call = %+v", c)
	}
	second, err := r.Next(ctx, nil, nil)
	if err != nil || second.Text != "Created lobby-display." || len(second.ToolCalls) != 0 {
		t.Fatalf("second turn = %+v, %v", second, err)
	}
	if _, err := r.Next(ctx, nil, nil); err == nil || !strings.Contains(err.Error(), "recording") {
		t.Errorf("past the end = %v, want an error naming the recording", err)
	}
}

// A recording that is empty, has a turn that is neither text nor calls, or
// names a call with no tool is refused when it is read, not mid-run.
func TestParseReplayRefusesMalformed(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]string{
		"empty":       "turns: []",
		"blank turn":  "turns:\n  - {}",
		"nameless":    "turns:\n  - tool_calls:\n      - arguments: {a: 1}",
		"not yaml":    "turns: [",
		"unknown key": "turns:\n  - txt: hi",
	} {
		if _, err := ParseReplay([]byte(doc)); err == nil {
			t.Errorf("%s: parsed, want an error", name)
		}
	}
}

// A positioned replay plays the turn after the model turns the conversation
// already holds, its calls numbered after the calls already made, so a run
// picked up from a stored conversation plays on from there, and the same
// conversation always gets the same turn.
func TestPositionedReplayPlaysFromTheConversation(t *testing.T) {
	t.Parallel()
	r, err := ParseReplay([]byte(recording))
	if err != nil {
		t.Fatal(err)
	}
	p := r.Positioned()
	ctx := context.Background()
	start := []Message{{Role: RoleUser, Text: "add a display"}}
	for range 2 {
		first, err := p.Next(ctx, start, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(first.ToolCalls) != 2 || first.ToolCalls[0].ID != "call-1" || first.ToolCalls[1].ID != "call-2" {
			t.Fatalf("first turn = %+v, want call-1 and call-2 every time", first)
		}
	}
	held := append(start,
		Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1", Name: "list-components"}, {ID: "call-2", Name: "create-component"}}},
		Message{Role: RoleTool, ToolCallID: "call-1", Text: "[]"},
		Message{Role: RoleTool, ToolCallID: "call-2", Text: "{}"},
	)
	second, err := p.Next(ctx, held, nil)
	if err != nil || second.Text != "Created lobby-display." {
		t.Fatalf("second turn = %+v, %v", second, err)
	}
	if _, err := p.Next(ctx, append(held, Message{Role: RoleAssistant, Text: "Created lobby-display."}), nil); err == nil || !strings.Contains(err.Error(), "recording") {
		t.Errorf("past the end = %v, want an error naming the recording", err)
	}
}

const scripted = `
turns:
  - text: The default answer.
conversations:
  - match: Triage an alarm
    turns:
      - tool_calls:
          - name: get-component
            arguments: {name: lobby-display}
      - text: The alarm is real.
`

// A positioned replay plays the conversation whose match the person's latest
// message holds, and the default turns for any other message; the position
// counts from that message, so a second prompt starts its own script.
func TestPositionedReplaySelectsByTheLatestMessage(t *testing.T) {
	t.Parallel()
	r, err := ParseReplay([]byte(scripted))
	if err != nil {
		t.Fatal(err)
	}
	p := r.Positioned()
	ctx := context.Background()

	other := []Message{{Role: RoleUser, Text: "what is down"}}
	if got, err := p.Next(ctx, other, nil); err != nil || got.Text != "The default answer." {
		t.Fatalf("other input = %+v, %v, want the default", got, err)
	}

	triage := append(other,
		Message{Role: RoleAssistant, Text: "The default answer."},
		Message{Role: RoleUser, Text: "# Triage an alarm\n\nThe component: lobby-display"},
	)
	first, err := p.Next(ctx, triage, nil)
	if err != nil || len(first.ToolCalls) != 1 || first.ToolCalls[0].Name != "get-component" || first.ToolCalls[0].ID != "call-1" {
		t.Fatalf("triage first turn = %+v, %v, want get-component as call-1", first, err)
	}
	held := append(triage,
		Message{Role: RoleAssistant, ToolCalls: first.ToolCalls},
		Message{Role: RoleTool, ToolCallID: "call-1", Text: "{}"},
	)
	if got, err := p.Next(ctx, held, nil); err != nil || got.Text != "The alarm is real." {
		t.Fatalf("triage second turn = %+v, %v", got, err)
	}
}

// A conversation with no match or no turns is refused when it is read.
func TestParseReplayRefusesMalformedConversations(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]string{
		"no match": "turns:\n  - text: hi\nconversations:\n  - turns:\n      - text: x",
		"no turns": "turns:\n  - text: hi\nconversations:\n  - match: x",
	} {
		if _, err := ParseReplay([]byte(doc)); err == nil {
			t.Errorf("%s: parsed, want an error", name)
		}
	}
}

// A turn that records a think waits that long before it arrives, in both the
// ordered and the positioned replay, so a capture paces like live inference;
// a run cancelled mid-think ends with the context's error.
func TestReplayThinksBeforeATurn(t *testing.T) {
	t.Parallel()
	r, err := ParseReplay([]byte(`
turns:
  - think: 60ms
    reasoning: Read the components first.
    tool_calls:
      - name: list-components
  - text: Done.
`))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	start := time.Now()
	first, err := r.Next(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Reasoning != "Read the components first." {
		t.Errorf("first turn's reasoning = %q, want the recorded thinking", first.Reasoning)
	}
	if d := time.Since(start); d < 60*time.Millisecond {
		t.Errorf("first turn arrived after %s, want at least its 60ms think", d)
	}
	start = time.Now()
	if _, err := r.Next(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("a turn with no think took %s", d)
	}

	start = time.Now()
	if _, err := r.Positioned().Next(ctx, []Message{{Role: RoleUser, Text: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 60*time.Millisecond {
		t.Errorf("positioned first turn arrived after %s, want at least 60ms", d)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := r.Positioned().Next(cancelled, []Message{{Role: RoleUser, Text: "hi"}}, nil); err != context.Canceled {
		t.Errorf("a cancelled think = %v, want context.Canceled", err)
	}
}

// A think that is not a duration, is negative or is over the cap is refused
// when the recording is read.
func TestParseReplayRefusesABadThink(t *testing.T) {
	t.Parallel()
	for _, think := range []string{"soon", "-1s", "31s"} {
		_, err := ParseReplay([]byte("turns:\n  - think: " + think + "\n    text: hi\n"))
		if err == nil || !strings.Contains(err.Error(), "think") {
			t.Errorf("think %q = %v, want an error naming the think", think, err)
		}
	}
}

const prompted = `
prompt: Tag the lobby display with its asset id
turns:
  - text: Tagged.
conversations:
  - prompt: Why is the lobby down?
    turns:
      - text: The lobby display lost power.
  - match: "# Triage an alarm"
    turns:
      - text: Triaged.
`

// A recording that names what its turns answer is a script of its own: it
// lists its prompts, plays a prompt however it is cased or punctuated, and
// does not play at all for a message none of its conversations answer (#2443).
func TestReplayAnswersOnlyItsRecordedPrompts(t *testing.T) {
	t.Parallel()
	r, err := ParseReplay([]byte(prompted))
	if err != nil {
		t.Fatal(err)
	}
	p := r.Positioned()
	sc, ok := p.(Scripted)
	if !ok {
		t.Fatal("a positioned replay is not Scripted")
	}
	want := []string{"Tag the lobby display with its asset id", "Why is the lobby down?"}
	if got := sc.Prompts(); !slices.Equal(got, want) {
		t.Errorf("prompts = %q, want %q", got, want)
	}
	for msg, plays := range map[string]bool{
		"Tag the lobby display with its asset id":      true,
		"  tag the LOBBY display   with its asset id.": true,
		"why is the lobby down":                        true,
		"# Triage an alarm\n\nThe component: lobby":    true,
		"Which alarm is worst?":                        false,
		"":                                             false,
	} {
		if got := sc.Plays(msg); got != plays {
			t.Errorf("Plays(%q) = %v, want %v", msg, got, plays)
		}
	}
	ctx := context.Background()
	got, err := p.Next(ctx, []Message{{Role: RoleUser, Text: "WHY is the lobby down"}}, nil)
	if err != nil || got.Text != "The lobby display lost power." {
		t.Errorf("a recorded prompt plays %+v, %v, want its conversation", got, err)
	}
	if got, err = p.Next(ctx, []Message{{Role: RoleUser, Text: "tag the lobby display with its asset id"}}, nil); err != nil || got.Text != "Tagged." {
		t.Errorf("the default prompt plays %+v, %v, want the default turns", got, err)
	}
	if _, err = p.Next(ctx, []Message{{Role: RoleUser, Text: "Which alarm is worst?"}}, nil); err == nil {
		t.Error("an unanswered message played a run, want an error")
	}
}

// A recording whose default turns name no prompt answers any message, as
// before, and lists only the prompts its conversations name.
func TestReplayWithoutADefaultPromptAnswersAnything(t *testing.T) {
	t.Parallel()
	r, err := ParseReplay([]byte(scripted))
	if err != nil {
		t.Fatal(err)
	}
	sc := r.Positioned().(Scripted)
	if len(sc.Prompts()) != 0 || !sc.Plays("what is down") {
		t.Errorf("prompts %q, plays %v, want none and any message played", sc.Prompts(), sc.Plays("what is down"))
	}
}

func TestParseReplayRefusesAConversationWithNeitherMatchNorPrompt(t *testing.T) {
	t.Parallel()
	if _, err := ParseReplay([]byte("turns:\n  - text: hi\nconversations:\n  - turns:\n      - text: x")); err == nil {
		t.Error("parsed, want an error")
	}
}
