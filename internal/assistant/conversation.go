package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
)

// Conversation is one of the person's stored conversations with omnibot
// (#2113), kept on the server so a run can be picked up later. It is read
// and written with the person's own token, and only its owner can reach it.
// The server keeps the transcript and the call a paused run waits on; it
// approves nothing, so whoever continues it decides that call in their own
// client (ADR-2113).
type Conversation struct {
	s    *Session
	name string
	// last is the seq of the last message this client has seen; an append
	// names it, so a client that fell behind is refused rather than
	// interleaved.
	last int
}

// OpenConversation reads the person's conversation by name, starting it
// when there is none: its messages so far, in the loop's own form, and the
// call a paused run left waiting, if any.
func (s *Session) OpenConversation(ctx context.Context, name string) (*Conversation, []provider.Message, *Pending, error) {
	var got struct {
		Pending  json.RawMessage `json:"pending"`
		Messages []struct {
			Seq  int             `json:"seq"`
			Body json.RawMessage `json:"body"`
		} `json:"messages"`
	}
	path := "/assistant/conversations/" + url.PathEscape(name)
	err := s.request(ctx, http.MethodGet, path, nil, http.StatusOK, &got)
	var status *statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		if err := s.request(ctx, http.MethodPost, "/assistant/conversations", map[string]string{"name": name}, http.StatusCreated, nil); err != nil {
			return nil, nil, nil, fmt.Errorf("omnibot: start the conversation %s: %w", name, err)
		}
		return &Conversation{s: s, name: name}, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("omnibot: read the conversation %s: %w", name, err)
	}
	c := &Conversation{s: s, name: name}
	msgs := make([]provider.Message, 0, len(got.Messages))
	for _, m := range got.Messages {
		var msg provider.Message
		if err := json.Unmarshal(m.Body, &msg); err != nil {
			return nil, nil, nil, fmt.Errorf("omnibot: the conversation %s holds a message omnibot cannot read (seq %d): %w", name, m.Seq, err)
		}
		msgs = append(msgs, msg)
		c.last = m.Seq
	}
	var pending *Pending
	if len(got.Pending) > 0 && string(got.Pending) != "null" {
		pending = &Pending{}
		if err := json.Unmarshal(got.Pending, pending); err != nil {
			return nil, nil, nil, fmt.Errorf("omnibot: the conversation %s holds a waiting call omnibot cannot read: %w", name, err)
		}
	}
	return c, msgs, pending, nil
}

// Append keeps msgs, the messages since the last append, after the last one
// this client saw, with the call the run paused on (nil clears it).
func (c *Conversation) Append(ctx context.Context, msgs []provider.Message, pending *Pending) error {
	if len(msgs) == 0 {
		return nil
	}
	type message struct {
		Role provider.Role    `json:"role"`
		Body provider.Message `json:"body"`
	}
	body := struct {
		After    int       `json:"after"`
		Messages []message `json:"messages"`
		Pending  *Pending  `json:"pending,omitempty"`
	}{After: c.last, Pending: pending}
	for _, m := range msgs {
		body.Messages = append(body.Messages, message{Role: m.Role, Body: m})
	}
	if err := c.s.request(ctx, http.MethodPost, "/assistant/conversations/"+url.PathEscape(c.name)+":append", body, http.StatusOK, nil); err != nil {
		return fmt.Errorf("omnibot: keep the conversation %s: %w", c.name, err)
	}
	c.last += len(msgs)
	return nil
}
