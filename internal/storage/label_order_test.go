package storage_test

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The label ordering rule is written twice, in SQL here and in TypeScript as
// byLabel (web/src/lib/entities.ts), and the console renders the server's order
// wherever the server serves the list (#757). That is only safe while the two
// halves are the same rule, so both run one committed fixture and assert one
// committed sequence: this test orders the rows through the real ListTags query,
// web/src/lib/entities.test.ts orders them through byLabel, and the expected
// order lives in the file both read. Changing the rule on one side fails the
// other side's test.
const labelOrderFixture = "../../web/src/lib/label_order.fixture.json"

type labelOrderCase struct {
	Rows  []struct{ Name, Label string } `json:"rows"`
	Order []string                       `json:"order"`
}

func TestTheSQLOrderingRuleMatchesTheCommittedLabelSequence(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(labelOrderFixture)
	if err != nil {
		t.Fatalf("read the shared fixture: %v", err)
	}
	var fx labelOrderCase
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse the shared fixture: %v", err)
	}

	gw := tagGateway(t)
	ctx := context.Background()
	for _, r := range fx.Rows {
		if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: r.Name, Label: r.Label}, all); err != nil {
			t.Fatalf("create tag %q: %v", r.Name, err)
		}
	}
	tags, err := gw.ListTags(ctx)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	// The seeded vocabulary carries rows of its own, so the assertion is on the
	// fixture's rows in the order the query returned them.
	var got []string
	for _, tg := range tags {
		if slices.Contains(fx.Order, tg.Name) {
			got = append(got, tg.Name)
		}
	}
	if !slices.Equal(got, fx.Order) {
		t.Fatalf("SQL order = %v, want the committed sequence %v", got, fx.Order)
	}
}
