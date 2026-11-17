package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/hyperscaleav/omniglass/internal/assistant"
)

// turns runs omnibot's prompts and, with --resume, keeps each finished turn
// on the server (#2113): the messages since the last one kept, appended after
// it.
type turns struct {
	loop *assistant.Loop
	conv *assistant.Conversation
	// kept is how many of the conversation's messages the server has.
	kept int
}

// resume picks up the named conversation, starting it when there is none. A
// call a paused run left waiting is decided first, put to the person as any
// ask is, and the run goes on from it before the first prompt.
func (t *turns) resume(ctx context.Context, sess *assistant.Session, name string, stdout, stderr io.Writer) error {
	conv, history, pending, err := sess.OpenConversation(ctx, name)
	if err != nil {
		return err
	}
	t.conv, t.kept = conv, len(history)
	t.loop.Resume(history)
	fmt.Fprintf(stderr, "omnibot: conversation %s, %d messages kept\n", name, len(history))
	if pending == nil {
		return nil
	}
	// The stored pending only says a call waits; what is put to the person is
	// the transcript's own open call, so the two cannot disagree.
	waiting, ok, err := t.loop.Waiting(ctx, history)
	if err != nil {
		return err
	}
	if !ok || waiting.CallID != pending.CallID {
		return fmt.Errorf("omnibot: the conversation %s says a call waits, but its transcript has no such open call", name)
	}
	approved := false
	if t.loop.Confirm != nil {
		if approved, err = t.loop.Confirm(ctx, waiting.Proposal); err != nil {
			return err
		}
	}
	out, err := t.loop.Continue(ctx, history, approved)
	if err != nil {
		return err
	}
	if err := t.keep(ctx, out); err != nil {
		return err
	}
	fmt.Fprintln(stdout, out.Text)
	return nil
}

// run is one prompt, kept when it finishes. A run that ends in error ends
// omnibot and is not kept: its last turn may hold a call with no answer.
func (t *turns) run(ctx context.Context, prompt string) (assistant.Outcome, error) {
	out, err := t.loop.Run(ctx, prompt)
	if err != nil {
		return out, err
	}
	return out, t.keep(ctx, out)
}

func (t *turns) keep(ctx context.Context, out assistant.Outcome) error {
	if t.conv == nil || len(out.Messages) <= t.kept {
		return nil
	}
	if err := t.conv.Append(ctx, out.Messages[t.kept:], out.Pending); err != nil {
		return err
	}
	t.kept = len(out.Messages)
	return nil
}
