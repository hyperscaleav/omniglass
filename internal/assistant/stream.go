package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/hyperscaleav/omniglass/internal/redact"
)

// RunInput is one run of a stored conversation on the server: a prompt, or
// the answer to the call the last run ended waiting on.
type RunInput struct {
	Prompt string
	// Answer, when set, resumes the run waiting on that call: Approved runs
	// it, else it is declined.
	Answer *Answer
}

// Answer names the call a resume run answers.
type Answer struct {
	CallID   string
	Approved bool
}

// StartConversation starts a conversation the server names, and answers
// its name.
func (s *Session) StartConversation(ctx context.Context) (string, error) {
	var got struct {
		Name string `json:"name"`
	}
	if err := s.request(ctx, http.MethodPost, "/assistant/conversations", map[string]string{}, http.StatusCreated, &got); err != nil {
		return "", fmt.Errorf("omnibot: start a conversation: %w", err)
	}
	return got.Name, nil
}

// Stream runs the named conversation on the server (POST
// /assistant/conversations/{name}:run) with the person's own token and
// answers its event stream, for the caller to read and close. The server
// runs the loop and holds the model's key; the client sees only the events.
// A refused run is an error carrying the server's reason.
func (s *Session) Stream(ctx context.Context, name string, in RunInput) (io.ReadCloser, error) {
	body := map[string]any{}
	if in.Answer != nil {
		status, payload := "cancelled", map[string]any{"approved": false}
		if in.Answer.Approved {
			status, payload = "resolved", map[string]any{"approved": true}
		}
		body["resume"] = []map[string]any{{"interruptId": in.Answer.CallID, "status": status, "payload": payload}}
	} else {
		body["messages"] = []map[string]any{{"role": "user", "content": in.Prompt}}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	path := "/assistant/conversations/" + url.PathEscape(name) + ":run"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.api.String()+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.person)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, errors.New(redact.Secrets(err.Error(), s.person))
	}
	if resp.StatusCode == http.StatusOK {
		return resp.Body, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var problem struct {
		Detail  string `json:"detail"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &problem)
	msg := problem.Detail
	if msg == "" {
		msg = problem.Message
	}
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	return nil, &statusError{code: resp.StatusCode, msg: fmt.Sprintf("omnibot: run %s: %d %s", name, resp.StatusCode, redact.Secrets(msg, s.person))}
}
