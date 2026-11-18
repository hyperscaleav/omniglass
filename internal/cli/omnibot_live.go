package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/assistant"
	"github.com/hyperscaleav/omniglass/internal/assistant/agui"
)

// live is omnibot run on the server (#2109): each prompt is a run of the
// stored conversation on POST /assistant/conversations/{name}:run, whose
// model and key the server holds, read back off its AG-UI stream. A call the
// person's policy asks about ends the run waiting on it; it is put to the
// person here and answered by a resume run.
type live struct {
	sess    *assistant.Session
	name    string
	confirm assistant.Confirmer
	stdout  io.Writer
	stderr  io.Writer
	// tools names each call by its id: a resume run's answered call started
	// in the run before it.
	tools map[string]string
	// notes are told to the model ahead of the next prompt.
	notes []string
}

// openLive picks up the named conversation, or starts one the server names
// when name is empty, and puts a call it was left waiting on to the person
// first.
func openLive(ctx context.Context, sess *assistant.Session, name string, confirm assistant.Confirmer, stdout, stderr io.Writer) (*live, error) {
	l := &live{sess: sess, name: name, confirm: confirm, stdout: stdout, stderr: stderr, tools: map[string]string{}}
	if name == "" {
		var err error
		if l.name, err = sess.StartConversation(ctx); err != nil {
			return nil, err
		}
		fmt.Fprintf(stderr, "omnibot: conversation %s (pick it up with --resume %s)\n", l.name, l.name)
		return l, nil
	}
	_, history, pending, err := sess.OpenConversation(ctx, name)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(stderr, "omnibot: conversation %s, %d messages kept\n", name, len(history))
	if pending == nil {
		return l, nil
	}
	l.tools[pending.CallID] = pending.Proposal.Tool
	if _, err := l.answer(ctx, *pending); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *live) tell(note string) { l.notes = append(l.notes, note) }

func (l *live) prompt(ctx context.Context, text string) (bool, error) {
	if len(l.notes) > 0 {
		text = strings.Join(l.notes, "\n\n") + "\n\n" + text
		l.notes = nil
	}
	return l.drive(ctx, assistant.RunInput{Prompt: text})
}

// answer puts the waiting call to the person and runs on from their answer.
// With no one to ask, it is declined.
func (l *live) answer(ctx context.Context, p assistant.Pending) (bool, error) {
	approved := false
	if l.confirm != nil {
		var err error
		if approved, err = l.confirm(ctx, p.Proposal); err != nil {
			return false, err
		}
	}
	return l.drive(ctx, assistant.RunInput{Answer: &assistant.Answer{CallID: p.CallID, Approved: approved}})
}

// drive runs once and, while the run ends waiting on a call, answers it and
// runs again: the model's text to stdout as it arrives, a line per call to
// stderr.
func (l *live) drive(ctx context.Context, in assistant.RunInput) (bool, error) {
	body, err := l.sess.Stream(ctx, l.name, in)
	if err != nil {
		return false, err
	}
	end, err := agui.Read(body, l.tools, func(e assistant.Event) {
		switch e.Kind {
		case assistant.EventText:
			fmt.Fprintln(l.stdout, e.Text)
		case assistant.EventResult:
			fmt.Fprintln(l.stderr, remoteLine(e))
		}
	})
	_ = body.Close()
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, err
	}
	if end.Waiting != nil {
		return l.answer(ctx, *end.Waiting)
	}
	return end.Stopped, nil
}

// remoteLine is a call's line from the server's stream. The stream says what
// the model was told, not whether a call that ran failed, so a call that ran
// carries no outcome.
func remoteLine(e assistant.Event) string {
	return fmt.Sprintf("omnibot: %s %s", e.Decision, e.Tool)
}

// errNoAnswer is a one-shot prompt that stopped at its step limit.
var errNoAnswer = errors.New("omnibot stopped without an answer")
