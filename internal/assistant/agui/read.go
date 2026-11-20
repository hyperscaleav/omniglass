package agui

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/assistant"
)

// Ending is how a narrated run ended: the model's last text, the call it
// waits on when it ended in an interrupt, and whether it stopped at its step
// ceiling.
type Ending struct {
	Text    string
	Waiting *assistant.Pending
	Stopped bool
	Steps   int
}

// wireEvent is any event this package writes, read back.
type wireEvent struct {
	Type         string   `json:"type"`
	MessageID    string   `json:"messageId"`
	Delta        string   `json:"delta"`
	ToolCallID   string   `json:"toolCallId"`
	ToolCallName string   `json:"toolCallName"`
	Content      string   `json:"content"`
	Message      string   `json:"message"`
	Outcome      *outcome `json:"outcome"`
	Result       *result  `json:"result"`
}

// Read reads a run this package narrated back off its stream (the CLI's
// side of #2121), telling hear each text and each call's result as they
// arrive. tools maps a call's id to its tool: it is filled from each call's
// start, and a resume run, whose answered call started in the run before,
// names that one in it. The stream carries no call's decision, so it is read
// from what the model is told: the declined and refused texts, else called.
// A run that ends in an error, or a stream that stops before the run ends, is
// an error.
func Read(r io.Reader, tools map[string]string, hear func(assistant.Event)) (Ending, error) {
	var end Ending
	text := map[string]*strings.Builder{}
	reasoning := map[string]*strings.Builder{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var ev wireEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return end, fmt.Errorf("omnibot: read the run: %w", err)
		}
		switch ev.Type {
		case typeTextMessageStart:
			text[ev.MessageID] = &strings.Builder{}
		case typeTextMessageContent:
			if b := text[ev.MessageID]; b != nil {
				b.WriteString(ev.Delta)
			}
		case typeTextMessageEnd:
			if b := text[ev.MessageID]; b != nil {
				end.Text = b.String()
				delete(text, ev.MessageID)
				hear(assistant.Event{Kind: assistant.EventText, Text: end.Text})
			}
		case typeReasoningStart:
			reasoning[ev.MessageID] = &strings.Builder{}
		case typeReasoningContent:
			if b := reasoning[ev.MessageID]; b != nil {
				b.WriteString(ev.Delta)
			}
		case typeReasoningEnd:
			if b := reasoning[ev.MessageID]; b != nil {
				delete(reasoning, ev.MessageID)
				hear(assistant.Event{Kind: assistant.EventReasoning, Text: b.String()})
			}
		case typeToolCallStart:
			tools[ev.ToolCallID] = ev.ToolCallName
		case typeToolCallResult:
			tool := tools[ev.ToolCallID]
			hear(assistant.Event{Kind: assistant.EventResult, CallID: ev.ToolCallID, Tool: tool, Decision: decisionOf(tool, ev.Content), Text: ev.Content})
		case typeRunError:
			return end, errors.New("omnibot: " + ev.Message)
		case typeRunFinished:
			if ev.Result != nil && ev.Result.Stopped == StoppedStepLimit {
				end.Stopped, end.Steps = true, ev.Result.Steps
			}
			if ev.Outcome != nil && ev.Outcome.Type == "interrupt" && len(ev.Outcome.Interrupts) > 0 {
				in := ev.Outcome.Interrupts[0]
				end.Waiting = &assistant.Pending{CallID: in.ID, Proposal: assistant.Proposal{
					Tool: in.Metadata.Tool, Title: in.Metadata.Title, Arguments: in.Metadata.Arguments, Destructive: in.Metadata.Destructive,
				}}
			}
			return end, nil
		}
	}
	if err := sc.Err(); err != nil {
		return end, fmt.Errorf("omnibot: read the run: %w", err)
	}
	return end, errors.New("omnibot: the stream closed before the run ended")
}

func decisionOf(tool, content string) assistant.Decision {
	switch content {
	case assistant.DeclinedText:
		return assistant.DecisionDeclined
	case assistant.RefusedText(tool):
		return assistant.DecisionRefused
	}
	return assistant.DecisionCalled
}
