package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/hyperscaleav/omniglass/internal/assistant"
	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
)

// openModel loads the recording that plays the model; a test swaps it to see
// what the model is sent.
var openModel = func(path string) (provider.Provider, error) { return provider.LoadReplay(path) }

// newOmnibotCmd is `omniglass omnibot` (#1166): the built-in assistant, a loop
// that drives the MCP server for the person running it. It mints its own
// one-day agent token from the person's, so everything it does is audited as
// omnibot's on the omnibot channel, holds no more than the person may, and
// cannot reach a plaintext secret. A tool whose policy is ask is put to the
// person on the terminal before it runs; with no terminal to ask, it does not
// run. This slice drives a recorded model (--replay); a live model is a later
// slice.
func newOmnibotCmd(version string) *cobra.Command {
	var replay, resume string
	var maxSteps int
	cmd := &cobra.Command{
		Use:   "omnibot [prompt...]",
		Short: "Run omnibot, the built-in assistant, on a prompt or in a chat",
		Long: "Runs omnibot, the built-in assistant, against the server's MCP tools as the person whose token it is " +
			"given. It mints a one-day agent token of its own for the omnibot channel, so the audit log shows its " +
			"actions as omnibot's acting for that person, and revokes the token when it finishes. A tool whose agent " +
			"policy is always runs, one whose policy is ask is put to you on the terminal first (without a terminal it " +
			"is declined), and one whose policy is never is not offered. With no prompt it opens a chat, each line " +
			"continuing the conversation; in the chat, /secret <name> <type> [location:<name>|component:<name>] asks " +
			"for a credential without echo, stores it as a secret with your own token, and tells omnibot only its " +
			"name, so the value never reaches the model. --resume <name> keeps the conversation on the server under that " +
			"name, for you alone, and picks it up where it was left; a call a paused run left waiting is put to you first. " +
			"The model is a recording (--replay) until a live provider lands.",
		Example: `  omniglass omnibot --replay lobby.yaml "add a display to the lobby"
  omniglass omnibot --replay lobby.yaml
  omniglass omnibot --replay lobby.yaml --resume lobby-refit`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, pos []string) error {
			if replay == "" {
				return errors.New("omnibot needs a model: pass --replay <recording.yaml> (a live model is not built yet)")
			}
			model, err := openModel(replay)
			if err != nil {
				return err
			}
			// Ctrl-C cancels the run rather than killing the process, so the
			// deferred Close still revokes omnibot's token however the run ends.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			server, token := connection(cmd)
			sess, err := assistant.Connect(ctx, server, token, version)
			if err != nil {
				return err
			}
			defer func() {
				if err := sess.Close(ctx); err != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), err)
				}
			}()
			con := newConsole(cmd.InOrStdin(), cmd.ErrOrStderr())
			loop := &assistant.Loop{
				Provider: model,
				Tools:    sess,
				MaxSteps: maxSteps,
				Confirm:  confirmerFor(cmd, con),
				Events: func(e assistant.Event) {
					if e.Kind == assistant.EventResult {
						fmt.Fprintln(cmd.ErrOrStderr(), observeLine(e))
					}
				},
			}
			t := &turns{loop: loop}
			if resume != "" {
				if err := t.resume(ctx, sess, resume, cmd.OutOrStdout(), con.out); err != nil {
					return err
				}
			}
			if len(pos) == 0 {
				return chat(ctx, con, cmd.OutOrStdout(), sess, t)
			}
			out, err := t.run(ctx, strings.Join(pos, " "))
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out.Text)
			if out.Stopped {
				return errors.New("omnibot stopped without an answer")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&replay, "replay", "", "a recorded model to drive the run (YAML turns)")
	cmd.Flags().StringVar(&resume, "resume", "", "keep the conversation on the server under this name, picking it up if it exists")
	cmd.Flags().IntVar(&maxSteps, "max-steps", 10, "the most model turns the run takes before it stops")
	return cmd
}

// confirmerFor asks on the command's input when that is a person: a terminal,
// or input a caller set in its place. Piped or redirected stdin has no one to
// ask, so every ask is declined.
func confirmerFor(cmd *cobra.Command, con *console) assistant.Confirmer {
	if f, ok := cmd.InOrStdin().(*os.File); ok {
		if st, err := f.Stat(); err != nil || st.Mode()&os.ModeCharDevice == 0 {
			return nil
		}
	}
	return terminalConfirmer(con)
}

// terminalConfirmer puts each proposal to the person on the console and
// reads the answer from it: only y or yes runs it. A run cancelled while it
// waits (an interrupt) stops waiting and ends with the cancellation.
func terminalConfirmer(con *console) assistant.Confirmer {
	return func(ctx context.Context, p assistant.Proposal) (bool, error) {
		args, _ := json.Marshal(p.Arguments)
		if p.Arguments == nil {
			args = []byte("{}")
		}
		kind := ""
		if p.Destructive {
			kind = " (destructive)"
		}
		title := p.Title
		if title == "" {
			title = p.Tool
		}
		fmt.Fprintf(con.out, "omnibot wants to run %s%s: %s\n  %s\nRun it? [y/N] ", p.Tool, kind, title, args)
		line, err := con.line(ctx)
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(con.out)
			return false, nil
		}
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true, nil
		}
		return false, nil
	}
}

// observeLine is a call's line on stderr, from its result event. Only a call
// that ran has an outcome; a declined or refused one never ran, so it carries
// none.
func observeLine(e assistant.Event) string {
	line := fmt.Sprintf("omnibot: %s %s", e.Decision, e.Tool)
	if e.Decision != assistant.DecisionCalled {
		return line
	}
	if e.IsError {
		return line + " (error)"
	}
	return line + " (ok)"
}
