// Package agui narrates an omnibot run as AG-UI events on a server-sent event
// stream (#2121): the protocol the console and any AG-UI client speak. The loop
// reports what it does as assistant.Events; a Run turns each into the AG-UI
// events that say the same thing, and ends the stream exactly once, in
// success, in an interrupt waiting on the person, or in an error.
package agui

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/hyperscaleav/omniglass/internal/assistant"
)

// The AG-UI event types a run emits.
const (
	typeRunStarted         = "RUN_STARTED"
	typeRunFinished        = "RUN_FINISHED"
	typeRunError           = "RUN_ERROR"
	typeTextMessageStart   = "TEXT_MESSAGE_START"
	typeTextMessageContent = "TEXT_MESSAGE_CONTENT"
	typeTextMessageEnd     = "TEXT_MESSAGE_END"
	typeToolCallStart      = "TOOL_CALL_START"
	typeToolCallArgs       = "TOOL_CALL_ARGS"
	typeToolCallEnd        = "TOOL_CALL_END"
	typeToolCallResult     = "TOOL_CALL_RESULT"
)

// ReasonToolApproval is the interrupt reason of a call waiting on the person.
const ReasonToolApproval = "tool_approval"

type runEvent struct {
	Type     string   `json:"type"`
	ThreadID string   `json:"threadId"`
	RunID    string   `json:"runId"`
	Outcome  *outcome `json:"outcome,omitempty"`
	Result   *result  `json:"result,omitempty"`
}

// result is what a finished run reports beyond its outcome: today only that
// it stopped at the loop's step ceiling without an answer.
type result struct {
	Stopped string `json:"stopped"`
	Steps   int    `json:"steps"`
}

// StoppedStepLimit is the result of a run that reached its step ceiling.
const StoppedStepLimit = "step_limit"

type outcome struct {
	Type       string      `json:"type"`
	Interrupts []interrupt `json:"interrupts,omitempty"`
}

type interrupt struct {
	ID             string         `json:"id"`
	Reason         string         `json:"reason"`
	Message        string         `json:"message"`
	ToolCallID     string         `json:"toolCallId"`
	ResponseSchema map[string]any `json:"responseSchema"`
	Metadata       interruptMeta  `json:"metadata"`
}

type interruptMeta struct {
	Tool        string         `json:"tool"`
	Arguments   map[string]any `json:"arguments,omitempty"`
	Title       string         `json:"title,omitempty"`
	Destructive bool           `json:"destructive"`
}

type errorEvent struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

type textEvent struct {
	Type      string `json:"type"`
	MessageID string `json:"messageId"`
	Role      string `json:"role,omitempty"`
	Delta     string `json:"delta,omitempty"`
}

type toolEvent struct {
	Type         string `json:"type"`
	ToolCallID   string `json:"toolCallId"`
	ToolCallName string `json:"toolCallName,omitempty"`
	Delta        string `json:"delta,omitempty"`
}

type resultEvent struct {
	Type       string `json:"type"`
	MessageID  string `json:"messageId"`
	ToolCallID string `json:"toolCallId"`
	Content    string `json:"content"`
	Role       string `json:"role"`
}

// approvalSchema is how an interrupt is answered: resume it with
// {"approved": true} to run the call, false (or a cancelled resume) to decline.
var approvalSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{"approved": map[string]any{"type": "boolean"}},
	"required":             []string{"approved"},
	"additionalProperties": false,
}

// Run narrates one run of a conversation (the AG-UI thread) onto w.
type Run struct {
	w        io.Writer
	threadID string
	runID    string
	messages int
	done     bool
	err      error
	result   *result
}

// NewRun narrates run runID of conversation threadID onto w, flushing after
// each event when w is an http.Flusher.
func NewRun(w io.Writer, threadID, runID string) *Run {
	return &Run{w: w, threadID: threadID, runID: runID}
}

// Err is the first write that failed, if any: the client went away.
func (r *Run) Err() error { return r.err }

// Start opens the run.
func (r *Run) Start() {
	r.emit(runEvent{Type: typeRunStarted, ThreadID: r.threadID, RunID: r.runID})
}

// Hear narrates one thing the loop did. A decision emits nothing of its own:
// a call that waits ends the run in Interrupt, and one that ran, was declined
// or was refused is told by its result.
func (r *Run) Hear(e assistant.Event) {
	switch e.Kind {
	case assistant.EventText:
		if e.Text == "" {
			return
		}
		id := r.messageID()
		r.emit(textEvent{Type: typeTextMessageStart, MessageID: id, Role: "assistant"})
		r.emit(textEvent{Type: typeTextMessageContent, MessageID: id, Delta: e.Text})
		r.emit(textEvent{Type: typeTextMessageEnd, MessageID: id})
	case assistant.EventProposed:
		args := []byte("{}")
		if e.Arguments != nil {
			if b, err := json.Marshal(e.Arguments); err == nil {
				args = b
			}
		}
		r.emit(toolEvent{Type: typeToolCallStart, ToolCallID: e.CallID, ToolCallName: e.Tool})
		r.emit(toolEvent{Type: typeToolCallArgs, ToolCallID: e.CallID, Delta: string(args)})
		r.emit(toolEvent{Type: typeToolCallEnd, ToolCallID: e.CallID})
	case assistant.EventResult:
		r.emit(resultEvent{Type: typeToolCallResult, MessageID: r.messageID(), ToolCallID: e.CallID, Content: e.Text, Role: "tool"})
	}
}

// Interrupt ends the run waiting on the person: the pending call, by its id,
// and the answer that resumes it.
func (r *Run) Interrupt(p assistant.Pending) {
	title := p.Title
	if title == "" {
		title = p.Tool
	}
	r.end(runEvent{Type: typeRunFinished, ThreadID: r.threadID, RunID: r.runID, Outcome: &outcome{
		Type: "interrupt",
		Interrupts: []interrupt{{
			ID:             p.CallID,
			Reason:         ReasonToolApproval,
			Message:        "omnibot asks before it runs " + title,
			ToolCallID:     p.CallID,
			ResponseSchema: approvalSchema,
			Metadata:       interruptMeta{Tool: p.Tool, Arguments: p.Arguments, Title: p.Title, Destructive: p.Destructive},
		}},
	}})
}

// Stopped marks the run as one that reached its step ceiling after steps
// without an answer; its Finish carries that as the run's result.
func (r *Run) Stopped(steps int) {
	r.result = &result{Stopped: StoppedStepLimit, Steps: steps}
}

// Finish ends the run in success, unless it has already ended.
func (r *Run) Finish() {
	r.end(runEvent{Type: typeRunFinished, ThreadID: r.threadID, RunID: r.runID, Outcome: &outcome{Type: "success"}, Result: r.result})
}

// Error ends the run on a failure, unless it has already ended.
func (r *Run) Error(message, code string) {
	r.end(errorEvent{Type: typeRunError, Message: message, Code: code})
}

func (r *Run) end(v any) {
	if r.done {
		return
	}
	r.emit(v)
	r.done = true
}

func (r *Run) messageID() string {
	r.messages++
	return r.runID + "-" + strconv.Itoa(r.messages)
}

// emit writes one event as an SSE data line. After the run ends, or after a
// write fails, it writes nothing.
func (r *Run) emit(v any) {
	if r.done || r.err != nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		r.err = err
		return
	}
	if _, err := io.WriteString(r.w, "data: "+string(b)+"\n\n"); err != nil {
		r.err = err
		return
	}
	if f, ok := r.w.(http.Flusher); ok {
		f.Flush()
	}
}
