package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/hyperscaleav/omniglass/internal/assistant"
)

// console is the person's side of omnibot: one reader for every line they
// type (a prompt, an answer to an ask, a credential), so no read takes a line
// meant for another.
type console struct {
	in  io.Reader
	r   *bufio.Reader
	out io.Writer
}

func newConsole(in io.Reader, out io.Writer) *console {
	return &console{in: in, r: bufio.NewReader(in), out: out}
}

// line reads the next line, without its newline. A cancelled ctx stops the
// wait and answers its error; io.EOF means the person is done typing.
func (c *console) line(ctx context.Context) (string, error) {
	type answer struct {
		line string
		err  error
	}
	got := make(chan answer, 1)
	go func() {
		line, err := c.r.ReadString('\n')
		got <- answer{line, err}
	}()
	select {
	case <-ctx.Done():
		fmt.Fprintln(c.out)
		return "", ctx.Err()
	case a := <-got:
		line := strings.TrimRight(a.line, "\r\n")
		if a.err != nil && (line == "" || !errors.Is(a.err, io.EOF)) {
			return "", a.err
		}
		return line, nil
	}
}

// hidden reads a line the terminal does not echo, so a credential never
// shows on screen. Input that is not a terminal has no echo to turn off and
// is read as a plain line. Echo comes back however the read ends, Ctrl-C
// included.
func (c *console) hidden(ctx context.Context) (string, error) {
	f, ok := c.in.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return c.line(ctx)
	}
	fd := int(f.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		return "", err
	}
	type answer struct {
		value []byte
		err   error
	}
	got := make(chan answer, 1)
	go func() {
		b, err := term.ReadPassword(fd)
		got <- answer{b, err}
	}()
	select {
	case <-ctx.Done():
		_ = term.Restore(fd, state)
		fmt.Fprintln(c.out)
		return "", ctx.Err()
	case a := <-got:
		fmt.Fprintln(c.out)
		return string(a.value), a.err
	}
}

// chatBanner is what the chat says before its first prompt.
const chatBanner = "omnibot: type a prompt; /exit or Ctrl-D ends.\n" +
	"/secret <name> <type> [location:<name>|component:<name>] hands omnibot a credential it can use but never read."

// chat is omnibot with no prompt on the command line: a conversation, a Run
// per line, each continuing the last. A /secret line hands it a credential.
func chat(ctx context.Context, con *console, stdout io.Writer, sess *assistant.Session, loop *assistant.Loop) error {
	fmt.Fprintln(con.out, chatBanner)
	for {
		fmt.Fprint(con.out, "> ")
		line, err := con.line(ctx)
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(con.out)
			return nil
		}
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case line == "/exit":
			return nil
		case line == "/secret" || strings.HasPrefix(line, "/secret "):
			if err := handCredential(ctx, con, sess, loop, strings.Fields(line)[1:]); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				fmt.Fprintln(con.out, err)
			}
			continue
		case strings.HasPrefix(line, "/"):
			fmt.Fprintf(con.out, "omnibot: no command %s; the commands are /secret and /exit\n", strings.Fields(line)[0])
			continue
		}
		out, err := loop.Run(ctx, line)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, out.Text)
		if out.Stopped {
			fmt.Fprintln(con.out, "omnibot: stopped without an answer")
		}
	}
}

// handCredential is /secret: it asks for the credential's values without
// echo, creates the secret with the person's own token, and tells the model
// only its reference.
func handCredential(ctx context.Context, con *console, sess *assistant.Session, loop *assistant.Loop, args []string) error {
	c, err := parseCredential(args)
	if err != nil {
		return err
	}
	fields, err := sess.CredentialFields(ctx, c.Type)
	if err != nil {
		return err
	}
	values := make(map[string]string, len(fields))
	for _, f := range fields {
		fmt.Fprintf(con.out, "%s: ", f.Name)
		read := con.line
		if f.Secret {
			read = con.hidden
		}
		v, err := read(ctx)
		if err != nil {
			return fmt.Errorf("omnibot: /secret: %w", err)
		}
		values[f.Name] = v
	}
	note, err := sess.HandCredential(ctx, c, values)
	if err != nil {
		return err
	}
	loop.Tell(note)
	fmt.Fprintf(con.out, "omnibot: stored the secret %s; omnibot knows its name, not its value\n", c.Name)
	return nil
}

// parseCredential reads /secret's arguments: a name, a type, and an owner
// (location:<name> or component:<name>) that is the platform when left out.
func parseCredential(args []string) (assistant.Credential, error) {
	const usage = "omnibot: usage: /secret <name> <type> [location:<name>|component:<name>]"
	if len(args) < 2 || len(args) > 3 {
		return assistant.Credential{}, errors.New(usage)
	}
	c := assistant.Credential{Name: args[0], Type: args[1], OwnerKind: "platform"}
	if len(args) == 3 {
		kind, name, ok := strings.Cut(args[2], ":")
		if !ok || name == "" || (kind != "location" && kind != "component") {
			return assistant.Credential{}, errors.New(usage)
		}
		c.OwnerKind, c.Owner = kind, name
	}
	return c, nil
}
