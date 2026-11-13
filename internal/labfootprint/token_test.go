package labfootprint

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSendPostsJSONAndDecodesTheAnswer: a write carries its body as JSON
// with the bearer token, and a 2xx answer decodes into out.
func TestSendPostsJSONAndDecodesTheAnswer(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "bad request shape", http.StatusBadRequest)
			return
		}
		var in struct{ Name string }
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "for-" + in.Name})
	}))
	t.Cleanup(srv.Close)
	h := &harness{http: srv.Client(), env: map[string]string{"LAB_TOKEN": "tok"}}
	var out struct{ Token string }
	code, err := h.send(context.Background(), http.MethodPost, srv.URL, map[string]string{"name": "edge"}, &out)
	if err != nil || code != http.StatusCreated || out.Token != "for-edge" {
		t.Fatalf("send = %d, %v, %+v", code, err, out)
	}
}

// TestSendReturnsARefusalsStatus: a non-2xx answer is an error, and its
// status comes back so a caller can wait out a conflict.
func TestSendReturnsARefusalsStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the node's simulator is not connected", http.StatusConflict)
	}))
	t.Cleanup(srv.Close)
	h := &harness{http: srv.Client(), env: map[string]string{"LAB_TOKEN": "tok"}}
	code, err := h.send(context.Background(), http.MethodPost, srv.URL, struct{}{}, nil)
	if code != http.StatusConflict || err == nil {
		t.Fatalf("send = %d, %v; want 409 and an error", code, err)
	}
}

// TestSendMintsAFreshTokenOnA401: a write refused for an expired token
// mints a fresh one and resends the same body once.
func TestSendMintsAFreshTokenOnA401(t *testing.T) {
	t.Parallel()
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if r.Header.Get("Authorization") != "Bearer new" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	h := &harness{http: srv.Client(), tokens: newTokenSource("old", func(context.Context) (string, error) { return "new", nil })}
	code, err := h.send(context.Background(), http.MethodDelete, srv.URL, map[string]int{"n": 1}, nil)
	if err != nil || code != http.StatusNoContent {
		t.Fatalf("send = %d, %v", code, err)
	}
	if len(bodies) != 2 || bodies[0] != `{"n":1}` || bodies[1] != bodies[0] {
		t.Fatalf("bodies = %q; want the same body twice", bodies)
	}
}
