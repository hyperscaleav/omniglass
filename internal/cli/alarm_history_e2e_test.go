package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAlarmListReadsClearedAlarmsFromTheAPI drives `omniglass alarm list` as
// an operator finding a cleared alarm (#2426): the default is what is raised
// now, --state cleared is the history with when each cleared, and --since after
// --until is refused. Skipped under -short.
func TestAlarmListReadsClearedAlarmsFromTheAPI(t *testing.T) {
	r := newPlatformHealthRig(t)
	call := func(method, path string, body string) []byte {
		t.Helper()
		req, err := http.NewRequest(method, r.url+path, strings.NewReader(body))
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+r.owner)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		var buf strings.Builder
		b := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(b)
			buf.Write(b[:n])
			if err != nil {
				break
			}
		}
		if resp.StatusCode >= 300 {
			t.Fatalf("%s %s = %d: %s", method, path, resp.StatusCode, buf.String())
		}
		return []byte(buf.String())
	}
	var raised struct {
		ID string `json:"id"`
	}
	raise := func(key string) string {
		t.Helper()
		out := call("POST", "/api/v1/components/lab-server/alarms", `{"severity":"high","message":"`+key+`","dedup_key":"`+key+`"}`)
		if err := json.Unmarshal(out, &raised); err != nil {
			t.Fatalf("decode raise: %v", err)
		}
		return raised.ID
	}
	gone := raise("gone-dark")
	raise("still-dark")
	call("DELETE", "/api/v1/components/lab-server/alarms/"+gone, "")

	list := func(args ...string) string {
		t.Helper()
		out, err := runCLI(t, append([]string{"alarm", "list", "--server", r.url, "--token", r.owner}, args...)...)
		if err != nil {
			t.Fatalf("omniglass alarm list %v: %v\n%s", args, err, out)
		}
		return out
	}
	if got := list(); !strings.Contains(got, "still-dark") || strings.Contains(got, "gone-dark") {
		t.Errorf("the default list should be the raised alarm alone:\n%s", got)
	}
	cleared := list("--state", "cleared")
	if !strings.Contains(cleared, "gone-dark") || strings.Contains(cleared, "still-dark") || !strings.Contains(cleared, "cleared_at") {
		t.Errorf("--state cleared should be the cleared alarm with its cleared_at:\n%s", cleared)
	}
	if got := list("--state", "all"); !strings.Contains(got, "gone-dark") || !strings.Contains(got, "still-dark") {
		t.Errorf("--state all should hold both:\n%s", got)
	}
	day := time.Now().UTC()
	if got := list("--state", "all", "--since", day.Add(-time.Hour).Format(time.RFC3339), "--until", day.Add(time.Hour).Format(time.RFC3339)); !strings.Contains(got, "gone-dark") {
		t.Errorf("a range around now should keep the cleared alarm:\n%s", got)
	}
	out, err := runCLI(t, "alarm", "list", "--since", day.Format(time.RFC3339), "--until", day.Add(-time.Hour).Format(time.RFC3339), "--server", r.url, "--token", r.owner)
	if err == nil {
		t.Errorf("--since after --until should be refused, got:\n%s", out)
	}
}
