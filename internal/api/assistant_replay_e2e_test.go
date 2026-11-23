package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

const promptedRecording = `prompt: Add a lobby display
turns:
  - text: Let me look.
    tool_calls: [{name: list-components}]
  - text: I will add it.
    tool_calls: [{name: create-component, arguments: {name: lobby-display, product: generic-device}}]
  - text: Done.
conversations:
  - prompt: What is in the lobby?
    turns:
      - text: Nothing yet.
`

func promptedModel(t *testing.T) api.Option {
	t.Helper()
	r, err := provider.ParseReplay([]byte(promptedRecording))
	if err != nil {
		t.Fatal(err)
	}
	return api.WithOmnibotModel(r.Positioned())
}

type omnibotFacts struct {
	Omnibot struct {
		Mode    string   `json:"mode"`
		Prompts []string `json:"prompts"`
	} `json:"omnibot"`
}

func readFacts(t *testing.T, p *people, tok string) omnibotFacts {
	t.Helper()
	var f omnibotFacts
	if err := json.Unmarshal(p.c.do(tok, http.MethodGet, "/assistant/conversations", nil, http.StatusOK), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// The conversations read says whether omnibot plays a recording or talks to a
// model, and which prompts the recording answers, and nothing of the model's
// configuration (#2443): a replay lists its prompts, a configured model is
// live with none, and a server with neither says none.
func TestOmnibotFactsSayReplayOrLive(t *testing.T) {
	t.Parallel()
	p := newPeople(t, promptedModel(t))
	p.person("olga", "operator", "all", "")

	f := readFacts(t, p, p.toks["olga"])
	if f.Omnibot.Mode != "replay" || len(f.Omnibot.Prompts) != 2 || f.Omnibot.Prompts[0] != "Add a lobby display" || f.Omnibot.Prompts[1] != "What is in the lobby?" {
		t.Fatalf("facts = %+v, want a replay of its two prompts", f)
	}

	if _, err := p.gw.CreateSecret(p.ctx, "", storage.SecretSpec{
		Name: "models-key", SecretType: "api-key", OwnerKind: "platform", Fields: map[string]string{"key": liveKey},
	}, scope.Set{All: true}, true); err != nil {
		t.Fatal(err)
	}
	p.c.do(p.owner, http.MethodPatch, "/settings/assistant", map[string]any{
		"provider": "openai", "base_url": "https://models.example.test/v1", "key_secret": "models-key", "model": "test-model",
	}, http.StatusOK)
	raw := string(p.c.do(p.toks["olga"], http.MethodGet, "/assistant/conversations", nil, http.StatusOK))
	var live omnibotFacts
	if err := json.Unmarshal([]byte(raw), &live); err != nil {
		t.Fatal(err)
	}
	if live.Omnibot.Mode != "live" || len(live.Omnibot.Prompts) != 0 {
		t.Fatalf("facts with a model = %+v, want live with no prompts", live)
	}
	for _, leak := range []string{liveKey, "models.example.test", "test-model", "models-key"} {
		if strings.Contains(raw, leak) {
			t.Errorf("the facts carry %q: %s", leak, raw)
		}
	}

	bare := newPeople(t)
	bare.person("olga", "operator", "all", "")
	if f := readFacts(t, bare, bare.toks["olga"]); f.Omnibot.Mode != "none" || f.Omnibot.Prompts == nil {
		t.Fatalf("facts with no model = %+v, want none and an empty list", f)
	}
}

// A replay plays a message it was recorded for and refuses any other with a
// 422 that says so and leaves the conversation as it was, never an unrelated
// run (#2443). The refusal is the server's: the recording is where it is
// decided.
func TestOmnibotReplayRefusesAMessageItDoesNotAnswer(t *testing.T) {
	t.Parallel()
	p := newPeople(t, promptedModel(t))
	p.person("olga", "operator", "all", "")
	tok := p.toks["olga"]
	p.c.do(tok, http.MethodPost, "/assistant/conversations", map[string]any{"name": "ask"}, http.StatusCreated)

	status, evs := p.run(tok, "ask", prompt("Which alarm is worst?"))
	body, _ := evs[0]["message"].(string)
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "This lab plays a recording; connect a model to ask your own question") ||
		!strings.Contains(body, "urn:omniglass:omnibot-replay-unmatched") {
		t.Fatalf("an unrecorded question: %d %s, want a 422 naming the recording", status, body)
	}
	var kept struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(p.c.do(tok, http.MethodGet, "/assistant/conversations/ask", nil, http.StatusOK), &kept); err != nil {
		t.Fatal(err)
	}
	if len(kept.Messages) != 0 {
		t.Errorf("the refused question left %d messages in the conversation", len(kept.Messages))
	}

	status, evs = p.run(tok, "ask", prompt("what is in the lobby"))
	if status != http.StatusOK || lastText(evs) != "Nothing yet." {
		t.Fatalf("a recorded question: %d %v, want its recorded answer", status, evs)
	}
}
