package labfootprint

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// tokenSource is the bearer token a measurement reads the API with, and how
// to mint another. A sampling run outlives a token's session (#1632), so a
// read refused with a 401 mints a fresh one and retries once.
type tokenSource struct {
	mu    sync.Mutex
	token string
	mint  func(context.Context) (string, error)
}

func newTokenSource(token string, mint func(context.Context) (string, error)) *tokenSource {
	return &tokenSource{token: token, mint: mint}
}

func (s *tokenSource) current() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

// refresh mints a token in place of stale. When another read already
// replaced stale, it returns that one, so concurrent refusals of one
// expired token mint once.
func (s *tokenSource) refresh(ctx context.Context, stale string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != stale {
		return s.token, nil
	}
	tok, err := s.mint(ctx)
	if err != nil {
		return "", fmt.Errorf("mint a fresh token after a 401: %w", err)
	}
	s.token = tok
	return tok, nil
}

// token is the bearer token reads carry: the source's, or the lab's.
func (h *harness) token() string {
	if h.tokens != nil {
		return h.tokens.current()
	}
	return h.env["LAB_TOKEN"]
}

// fetch GETs u with the bearer token and reads the whole body. A 401 with a
// token source behind it mints a fresh token and retries once. Any status
// is returned with its body; only a transport failure is an error.
func (h *harness) fetch(ctx context.Context, u string) (int, string, []byte, error) {
	tok := h.token()
	code, status, body, err := h.fetchWith(ctx, u, tok)
	if err != nil || code != http.StatusUnauthorized || h.tokens == nil {
		return code, status, body, err
	}
	if tok, err = h.tokens.refresh(ctx, tok); err != nil {
		return 0, "", nil, err
	}
	return h.fetchWith(ctx, u, tok)
}

func (h *harness) fetchWith(ctx context.Context, u, tok string) (int, string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := h.http.Do(req)
	if err != nil {
		return 0, "", nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", nil, err
	}
	return resp.StatusCode, resp.Status, body, nil
}

// statusError is a non-2xx answer as an error: its status and the start of
// its body.
func statusError(status string, body []byte) error {
	if len(body) > 512 {
		body = body[:512]
	}
	return fmt.Errorf("%s: %s", status, strings.TrimSpace(string(body)))
}
