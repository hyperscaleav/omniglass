package storage_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The alarm fact map is declared once (#618): a notification template reads
// its keys, and an action rule's predicate reads the same facts in
// snake_case. A key added to one and not the other is a template and a
// predicate seeing two different alarms.
func TestTheActionSiteBindsEveryAlarmFact(t *testing.T) {
	var bindings []string
	for _, b := range rules.ActionEnv().Bindings() {
		bindings = append(bindings, b.Name)
	}
	camel := regexp.MustCompile(`([a-z])([A-Z])`)
	for _, k := range storage.AlarmFactKeys() {
		snake := strings.ToLower(camel.ReplaceAllString(k, "${1}_${2}"))
		if !slices.Contains(bindings, snake) {
			t.Errorf("the alarm fact %s has no %s binding at the action site", k, snake)
		}
	}
	// The one binding beyond the facts is the severity's rank.
	if len(bindings) != len(storage.AlarmFactKeys())+1 || !slices.Contains(bindings, "severity_rank") {
		t.Errorf("action site bindings = %v, want the facts plus severity_rank", bindings)
	}
}

// The docs render the fact map from the generated file: it is the
// declaration, rendered, or make gen was not run.
func TestNotificationFactsMatchCommitted(t *testing.T) {
	fresh, err := storage.NotificationFactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("../../docs/src/generated/notificationfacts.json")
	if err != nil {
		t.Fatalf("read committed notificationfacts.json (run make gen and commit it): %v", err)
	}
	if string(committed) != string(fresh) {
		t.Error("docs/src/generated/notificationfacts.json drifted from the alarm fact map: run make gen and commit the result")
	}
}
