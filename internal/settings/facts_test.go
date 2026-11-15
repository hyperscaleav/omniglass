package settings

import (
	"encoding/json"
	"os"
	"testing"
)

// factRow is the test's view of one row of the facts document, decoded from the
// JSON so the test pins the published shape, not the Go type behind it.
type factRow struct {
	Namespace     string   `json:"namespace"`
	Key           string   `json:"key"`
	Type          string   `json:"type"`
	Items         string   `json:"items"`
	Default       any      `json:"default"`
	Enum          []string `json:"enum"`
	Pattern       string   `json:"pattern"`
	Minimum       *float64 `json:"minimum"`
	Maximum       *float64 `json:"maximum"`
	Nullable      *bool    `json:"nullable"`
	Doc           string   `json:"doc"`
	Domain        string   `json:"domain"`
	ClientVisible bool     `json:"client_visible"`
}

func factRows(t *testing.T) []factRow {
	t.Helper()
	b, err := FactsJSON()
	if err != nil {
		t.Fatalf("FactsJSON: %v", err)
	}
	var doc struct {
		Settings []factRow `json:"settings"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("facts JSON does not parse: %v", err)
	}
	return doc.Settings
}

func factRowFor(t *testing.T, ns, key string) factRow {
	t.Helper()
	for _, r := range factRows(t) {
		if r.Namespace == ns && r.Key == key {
			return r
		}
	}
	t.Fatalf("no facts row for %s.%s", ns, key)
	return factRow{}
}

// TestFactsRowPerTagKind pins the row shape for a representative key per tag kind.
func TestFactsRowPerTagKind(t *testing.T) {
	t.Run("enum", func(t *testing.T) {
		r := factRowFor(t, "ui", "theme")
		if r.Type != "string" || r.Default != "omniglass-dark" || len(r.Enum) != 2 || r.Enum[0] != "omniglass-dark" || r.Enum[1] != "omniglass-light" {
			t.Errorf("ui.theme row = %+v", r)
		}
		if r.Domain != "profile" || !r.ClientVisible || r.Doc != "Console color theme" {
			t.Errorf("ui.theme domain/visibility/doc = %q %v %q", r.Domain, r.ClientVisible, r.Doc)
		}
	})
	t.Run("pattern", func(t *testing.T) {
		r := factRowFor(t, "ui", "default_landing")
		if r.Pattern != "^/" || r.Default != "/" {
			t.Errorf("ui.default_landing row = %+v", r)
		}
	})
	t.Run("range", func(t *testing.T) {
		r := factRowFor(t, "retention", "metric_days")
		if r.Type != "integer" || r.Default != float64(14) || r.Minimum == nil || *r.Minimum != 1 || r.Maximum == nil || *r.Maximum != 36500 {
			t.Errorf("retention.metric_days row = %+v", r)
		}
		if r.Domain != "platform" || !r.ClientVisible {
			t.Errorf("retention.metric_days domain/visibility = %q %v", r.Domain, r.ClientVisible)
		}
	})
	t.Run("zero minimum is kept", func(t *testing.T) {
		r := factRowFor(t, "database", "slow_query_ms")
		if r.Minimum == nil || *r.Minimum != 0 {
			t.Errorf("database.slow_query_ms minimum = %v, want a present 0", r.Minimum)
		}
	})
	t.Run("list default and nullable false", func(t *testing.T) {
		r := factRowFor(t, "label", "acronyms")
		list, ok := r.Default.([]any)
		if r.Type != "array" || r.Items != "string" || !ok || len(list) == 0 || list[0] != "AV" {
			t.Errorf("label.acronyms row = %+v", r)
		}
		if r.Nullable == nil || *r.Nullable {
			t.Errorf("label.acronyms nullable = %v, want a present false", r.Nullable)
		}
	})
	t.Run("empty list default is an empty list", func(t *testing.T) {
		r := factRowFor(t, "notification", "egress_allow")
		if list, ok := r.Default.([]any); !ok || len(list) != 0 {
			t.Errorf("notification.egress_allow default = %#v, want []", r.Default)
		}
	})
	t.Run("empty string default is kept, not dropped", func(t *testing.T) {
		r := factRowFor(t, "notification", "mail_relay")
		if r.Default != "" {
			t.Errorf("notification.mail_relay default = %#v, want the empty string", r.Default)
		}
		if r.Domain != "platform" || r.ClientVisible {
			t.Errorf("notification.mail_relay domain/visibility = %q %v", r.Domain, r.ClientVisible)
		}
	})
	t.Run("no default tag means no default", func(t *testing.T) {
		r := factRowFor(t, "retention", "node_log_debug_days")
		if r.Default != nil {
			t.Errorf("node_log_debug_days default = %#v, want absent", r.Default)
		}
		if r.Key != "node_log_debug_days" {
			t.Errorf("key = %q: the json tag's omitempty option leaked into the name", r.Key)
		}
	})
}

// TestFactsCoverEveryKey keeps the facts and the reflected defaults one set: a
// key with a default tag appears with it, in the declaration order of the struct.
func TestFactsCoverEveryKey(t *testing.T) {
	rows := factRows(t)
	defaults := Defaults()
	withDefault := 0
	for _, r := range rows {
		if r.Doc == "" {
			t.Errorf("%s.%s has no doc string", r.Namespace, r.Key)
		}
		if _, ok := defaults[r.Namespace][r.Key]; ok {
			withDefault++
			if r.Default == nil {
				t.Errorf("%s.%s has a declared default the facts omit", r.Namespace, r.Key)
			}
		}
	}
	want := 0
	for _, m := range defaults {
		want += len(m)
	}
	if withDefault != want {
		t.Errorf("facts carry %d defaults, Defaults() has %d", withDefault, want)
	}
	if rows[0].Namespace != "ui" || rows[0].Key != "theme" {
		t.Errorf("first row = %s.%s, want declaration order starting at ui.theme", rows[0].Namespace, rows[0].Key)
	}
}

// TestSettingsFactsMatchesCommitted is the drift gate for the generated settings
// reference: the committed docs/src/generated/settings.json must equal a fresh
// render of the schema, so a changed tag fails here until make gen is run.
func TestSettingsFactsMatchesCommitted(t *testing.T) {
	fresh, err := FactsJSON()
	if err != nil {
		t.Fatalf("FactsJSON: %v", err)
	}
	committed, err := os.ReadFile("../../docs/src/generated/settings.json")
	if err != nil {
		t.Fatalf("read committed settings.json (run make gen and commit it): %v", err)
	}
	if string(committed) != string(fresh) {
		t.Error("docs/src/generated/settings.json drifted from the settings schema: run make gen and commit the result")
	}
}
