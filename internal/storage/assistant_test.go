package storage_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// Omnibot conversations (#2113), against a real Postgres: a conversation is
// its owner's alone (anyone else, an owner-role user included, finds
// nothing), an append names the last message it saw so two clients cannot
// interleave, a paused run's pending write is kept with it, and retention
// prunes a conversation by when it last moved.

func msg(role, text string) storage.AssistantMessage {
	body, _ := json.Marshal(map[string]string{"text": text})
	return storage.AssistantMessage{Role: role, Body: body}
}

func TestAConversationIsItsOwnersAlone(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	bob, alice, root := f.ids["bob"], f.ids["alice"], f.ids["root"]

	c, err := f.gw.CreateAssistantConversation(f.ctx, bob, bob, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name == "" {
		t.Fatal("a conversation created with no name has none; the server should name it")
	}
	if _, err := f.gw.AppendAssistantMessages(f.ctx, bob, bob, c.Name, storage.AssistantAppend{
		After: 0, Messages: []storage.AssistantMessage{msg("user", "hi"), msg("assistant", "hello")},
	}); err != nil {
		t.Fatal(err)
	}

	for who, id := range map[string]string{"alice": alice, "root": root, "not-a-uuid": "x"} {
		if _, _, err := f.gw.GetAssistantConversation(f.ctx, id, c.Name); !errors.Is(err, storage.ErrAssistantConversationNotFound) {
			t.Errorf("%s get bob's conversation = %v, want not found", who, err)
		}
		if _, err := f.gw.AppendAssistantMessages(f.ctx, id, id, c.Name, storage.AssistantAppend{After: 2, Messages: []storage.AssistantMessage{msg("user", "x")}}); !errors.Is(err, storage.ErrAssistantConversationNotFound) {
			t.Errorf("%s append to bob's conversation = %v, want not found", who, err)
		}
		if err := f.gw.DeleteAssistantConversation(f.ctx, id, id, c.Name); !errors.Is(err, storage.ErrAssistantConversationNotFound) {
			t.Errorf("%s delete bob's conversation = %v, want not found", who, err)
		}
		list, err := f.gw.ListAssistantConversations(f.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 0 {
			t.Errorf("%s lists %d conversations, want none of bob's", who, len(list))
		}
	}

	got, msgs, err := f.gw.GetAssistantConversation(f.ctx, bob, c.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != c.ID || len(msgs) != 2 || msgs[0].Seq != 1 || msgs[1].Seq != 2 || msgs[1].Role != "assistant" {
		t.Fatalf("bob's own conversation = %+v with %+v, want his two messages in order", got, msgs)
	}
	list, err := f.gw.ListAssistantConversations(f.ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != c.Name {
		t.Fatalf("bob lists %+v, want his one conversation", list)
	}
	if err := f.gw.DeleteAssistantConversation(f.ctx, bob, bob, c.Name); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.gw.GetAssistantConversation(f.ctx, bob, c.Name); !errors.Is(err, storage.ErrAssistantConversationNotFound) {
		t.Fatalf("a deleted conversation reads %v, want not found", err)
	}
}

func TestAConversationNameIsOnePerOwner(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	bob, alice := f.ids["bob"], f.ids["alice"]
	if _, err := f.gw.CreateAssistantConversation(f.ctx, bob, bob, "triage"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateAssistantConversation(f.ctx, bob, bob, "triage"); !errors.Is(err, storage.ErrAssistantConversationExists) {
		t.Fatalf("bob's second triage = %v, want exists", err)
	}
	if _, err := f.gw.CreateAssistantConversation(f.ctx, alice, alice, "triage"); err != nil {
		t.Fatalf("alice's own triage = %v; a name is unique per owner, not platform-wide", err)
	}
	if _, err := f.gw.CreateAssistantConversation(f.ctx, bob, bob, "Not A Name"); !errors.Is(err, storage.ErrInvalidEntityName) {
		t.Fatalf("an illegal name = %v, want invalid", err)
	}
}

func TestAnAppendNamesTheLastMessageItSaw(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	bob := f.ids["bob"]
	c, err := f.gw.CreateAssistantConversation(f.ctx, bob, bob, "chat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.AppendAssistantMessages(f.ctx, bob, bob, "chat", storage.AssistantAppend{
		After: 0, Messages: []storage.AssistantMessage{msg("user", "one")}, Usage: storage.AssistantUsage{InputTokens: 10, OutputTokens: 3},
	}); err != nil {
		t.Fatal(err)
	}
	// A second client that last saw an empty conversation is refused, not
	// interleaved.
	if _, err := f.gw.AppendAssistantMessages(f.ctx, bob, bob, "chat", storage.AssistantAppend{
		After: 0, Messages: []storage.AssistantMessage{msg("user", "stale")},
	}); !errors.Is(err, storage.ErrAssistantConversationMoved) {
		t.Fatalf("a stale append = %v, want moved", err)
	}
	pending := json.RawMessage(`{"call_id":"call-1","tool":"bind_secret"}`)
	got, err := f.gw.AppendAssistantMessages(f.ctx, bob, bob, "chat", storage.AssistantAppend{
		After: 1, Messages: []storage.AssistantMessage{msg("assistant", "two")}, Pending: pending,
		Usage: storage.AssistantUsage{InputTokens: 5, OutputTokens: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != c.ID || got.Usage.InputTokens != 15 || got.Usage.OutputTokens != 5 {
		t.Fatalf("after two appends = %+v, want usage totaled 15 in, 5 out", got)
	}
	var p map[string]string
	if err := json.Unmarshal(got.Pending, &p); err != nil || p["call_id"] != "call-1" {
		t.Fatalf("pending = %s (%v), want the paused call kept", got.Pending, err)
	}
	// The next append that does not carry a pending clears it: the call was
	// answered.
	got, err = f.gw.AppendAssistantMessages(f.ctx, bob, bob, "chat", storage.AssistantAppend{
		After: 2, Messages: []storage.AssistantMessage{msg("tool", "bound")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Pending != nil {
		t.Fatalf("pending after an answer = %s, want cleared", got.Pending)
	}
	// An audit row records each write, and never a message's content.
	conn := connectDSN(t, f.dsn)
	if n := count(t, conn, `select count(*) from audit_log where resource = 'assistant_conversation' and resource_id = '`+c.ID+`'`); n != 4 {
		t.Fatalf("audit rows = %d, want create and three appends", n)
	}
	if n := count(t, conn, `select count(*) from audit_log where resource = 'assistant_conversation' and (new::text like '%bound%' or new::text like '%one%')`); n != 0 {
		t.Fatalf("%d audit rows carry message content, want none", n)
	}
}

// A server run claims the conversation before any tool runs (#2121): the
// claim names the last message the run read and holds a lease until the run
// appends or releases it, so a second run, a resume racing a resume included,
// is refused before it acts. A lease that ran out is claimable again, so a
// run that died does not lock its conversation for good.
func TestARunClaimsItsConversation(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	bob := f.ids["bob"]
	if _, err := f.gw.CreateAssistantConversation(f.ctx, bob, bob, "chat"); err != nil {
		t.Fatal(err)
	}
	soon := time.Now().Add(time.Minute)
	if err := f.gw.ClaimAssistantRun(f.ctx, bob, "chat", storage.AssistantRunClaim{Run: "r1", After: 0, Until: soon}); err != nil {
		t.Fatalf("claim = %v", err)
	}
	if err := f.gw.ClaimAssistantRun(f.ctx, bob, "chat", storage.AssistantRunClaim{Run: "r2", After: 0, Until: soon}); !errors.Is(err, storage.ErrAssistantConversationBusy) {
		t.Fatalf("a second claim = %v, want busy", err)
	}
	if err := f.gw.ClaimAssistantRun(f.ctx, f.ids["alice"], "chat", storage.AssistantRunClaim{Run: "r3", Until: soon}); !errors.Is(err, storage.ErrAssistantConversationNotFound) {
		t.Fatalf("another's claim = %v, want not found", err)
	}
	// An append by another run, or by no run (a client keeping its own
	// loop's transcript), is refused while the lease is r1's; r1's append
	// lands and frees the conversation.
	for _, run := range []string{"r2", ""} {
		if _, err := f.gw.AppendAssistantMessages(f.ctx, bob, bob, "chat", storage.AssistantAppend{
			Run: run, After: 0, Messages: []storage.AssistantMessage{msg("user", "two")},
		}); !errors.Is(err, storage.ErrAssistantConversationBusy) {
			t.Fatalf("an append by run %q under r1's lease = %v, want busy", run, err)
		}
	}
	if _, err := f.gw.AppendAssistantMessages(f.ctx, bob, bob, "chat", storage.AssistantAppend{
		Run: "r1", After: 0, Messages: []storage.AssistantMessage{msg("user", "one")},
	}); err != nil {
		t.Fatalf("the claimant's append = %v", err)
	}
	// A claim names the last message it read: one that read before the
	// append is stale.
	if err := f.gw.ClaimAssistantRun(f.ctx, bob, "chat", storage.AssistantRunClaim{Run: "r4", After: 0, Until: soon}); !errors.Is(err, storage.ErrAssistantConversationMoved) {
		t.Fatalf("a stale claim = %v, want moved", err)
	}
	if err := f.gw.ClaimAssistantRun(f.ctx, bob, "chat", storage.AssistantRunClaim{Run: "r5", After: 1, Until: soon}); err != nil {
		t.Fatalf("a claim after the append = %v", err)
	}
	// Release frees it; releasing a lease that is not yours changes nothing.
	if err := f.gw.ReleaseAssistantRun(f.ctx, bob, "chat", "r9"); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.ClaimAssistantRun(f.ctx, bob, "chat", storage.AssistantRunClaim{Run: "r6", After: 1, Until: soon}); !errors.Is(err, storage.ErrAssistantConversationBusy) {
		t.Fatalf("a claim after a stranger's release = %v, want busy", err)
	}
	if err := f.gw.ReleaseAssistantRun(f.ctx, bob, "chat", "r5"); err != nil {
		t.Fatal(err)
	}
	gone := time.Now().Add(-time.Second)
	if err := f.gw.ClaimAssistantRun(f.ctx, bob, "chat", storage.AssistantRunClaim{Run: "r7", After: 1, Until: gone}); err != nil {
		t.Fatalf("a claim after release = %v", err)
	}
	// r7's lease has run out: a plain append lands over it, and the next
	// claim takes the conversation.
	if _, err := f.gw.AppendAssistantMessages(f.ctx, bob, bob, "chat", storage.AssistantAppend{
		After: 1, Messages: []storage.AssistantMessage{msg("user", "three")},
	}); err != nil {
		t.Fatalf("an append over a lapsed lease = %v", err)
	}
	if err := f.gw.ClaimAssistantRun(f.ctx, bob, "chat", storage.AssistantRunClaim{Run: "r8", After: 2, Until: soon}); err != nil {
		t.Fatalf("a claim over a lapsed lease = %v", err)
	}
}

func TestRetentionPrunesAConversationByWhenItLastMoved(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	bob := f.ids["bob"]
	for _, n := range []string{"old", "fresh"} {
		if _, err := f.gw.CreateAssistantConversation(f.ctx, bob, bob, n); err != nil {
			t.Fatal(err)
		}
		if _, err := f.gw.AppendAssistantMessages(f.ctx, bob, bob, n, storage.AssistantAppend{After: 0, Messages: []storage.AssistantMessage{msg("user", n)}}); err != nil {
			t.Fatal(err)
		}
	}
	conn := connectDSN(t, f.dsn)
	mustExec(t, conn, `update assistant_conversation set updated_at = now() - interval '40 days' where name = 'old'`)

	n, err := f.gw.PruneAssistantConversations(f.ctx, time.Now().Add(-30*24*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pruned %d, want the one idle 40 days", n)
	}
	if left := count(t, conn, `select count(*) from assistant_message`); left != 1 {
		t.Fatalf("%d messages left, want the fresh conversation's one", left)
	}
	set, err := f.gw.AssistantSettings(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if set.ConversationDays != 30 {
		t.Fatalf("conversation_days = %d, want the default 30", set.ConversationDays)
	}
}

// The migration round-trips: down drops both tables and up brings them back.
func TestAssistantConversationMigrationRoundTrips(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration: needs a Postgres testcontainer")
	}
	dsn := storagetest.NewDSN(t)
	conn := connectDSN(t, dsn)
	if err := migrate.RollbackBelow(dsn, "20261117180000"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, conn, `select count(*) from information_schema.tables where table_name in ('assistant_conversation','assistant_message')`); n != 0 {
		t.Fatalf("after down, %d assistant tables remain", n)
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatal(err)
	}
	if n := count(t, conn, `select count(*) from information_schema.tables where table_name in ('assistant_conversation','assistant_message')`); n != 2 {
		t.Fatalf("after up, %d assistant tables, want 2", n)
	}
	const runCols = `select count(*) from information_schema.columns where table_name = 'assistant_conversation' and column_name in ('run_id','run_until')`
	if err := migrate.RollbackBelow(dsn, "20261117210000"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, conn, runCols); n != 0 {
		t.Fatalf("after down, %d run columns remain", n)
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatal(err)
	}
	if n := count(t, conn, runCols); n != 2 {
		t.Fatalf("after up, %d run columns, want 2", n)
	}
}
