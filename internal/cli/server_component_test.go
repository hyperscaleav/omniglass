package cli

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// ensureFunc is a component ensure answering as the test says.
type ensureFunc func(ctx context.Context, identity string) (string, error)

func (f ensureFunc) EnsureServerComponent(ctx context.Context, identity string) (string, error) {
	return f(ctx, identity)
}

// TestServerComponentIsTheEnsuredOne: a boot whose ensure answers reports on
// the component it named, under the identity it was given.
func TestServerComponentIsTheEnsuredOne(t *testing.T) {
	t.Parallel()
	var asked string
	got := serverComponent(context.Background(), ensureFunc(func(_ context.Context, id string) (string, error) {
		asked = id
		return "c-1", nil
	}), "og-1", slog.New(slog.DiscardHandler))
	if got != "c-1" || asked != "og-1" {
		t.Fatalf("serverComponent = %q (asked for %q), want c-1 for og-1", got, asked)
	}
}

// TestAFailedEnsureWarnsAndReportsNothing (#1671): a boot whose ensure fails
// goes on with no component, so no platform figure lands, and says so as a
// warning naming the identity and the cause.
func TestAFailedEnsureWarnsAndReportsNothing(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	got := serverComponent(context.Background(), ensureFunc(func(context.Context, string) (string, error) {
		return "", errors.New("no platform system")
	}), "og-1", slog.New(slog.NewTextHandler(&buf, nil)))
	if got != "" {
		t.Fatalf("serverComponent = %q after a failed ensure, want empty", got)
	}
	line := buf.String()
	for _, want := range []string{"level=WARN", "og-1", "no platform system"} {
		if !strings.Contains(line, want) {
			t.Errorf("warning %q does not carry %q", line, want)
		}
	}
}
