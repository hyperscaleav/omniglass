package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The storage half of the on-demand node run (#1163): a spec checked as a
// save would, a preview planned into the tasks a node runs (and audited), and
// an endpoint's poll resolved for a run now. Skipped under -short.

const previewPassword = "preview-s3cret-value"

func runStack(t *testing.T) (*storage.PG, context.Context) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-p"}, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "display-login", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "admin", "password": previewPassword},
	}, all, true); err != nil {
		t.Fatal(err)
	}
	return gw, ctx
}

func previewOf(driverRef string, inputs map[string]string) storage.PreviewInput {
	all := scope.Set{All: true}
	return storage.PreviewInput{Node: "edge-p", Driver: driverRef, Inputs: inputs, NodeRead: all, SecretReveal: all, CanAdmin: true}
}

func TestDriverSpecFindings(t *testing.T) {
	gw, ctx := runStack(t)
	good, err := gw.GetDriver(ctx, "boreal-edge-web")
	if err != nil {
		t.Fatal(err)
	}
	if f, err := gw.DriverSpecFindings(ctx, good.Spec); err != nil || len(f) != 0 {
		t.Fatalf("findings of a shipped spec = %+v, %v; want none", f, err)
	}
	broken := strings.Replace(string(good.Spec), `"model-number"`, `"no-such-key"`, 1)
	f, err := gw.DriverSpecFindings(ctx, []byte(broken))
	if err != nil || len(f) == 0 || f[0].Function != "identity" || !strings.Contains(f[0].Message, "no-such-key") {
		t.Fatalf("findings of a spec emitting an unknown key = %+v, %v", f, err)
	}
}

// A preview of a stored driver becomes a task per poll, as a worklist would
// deliver it: the dial target from the inputs, the secret unsealed, and the
// asking audited on the node and on the secret.
func TestPlanDriverPreview(t *testing.T) {
	gw, ctx := runStack(t)
	plan, err := gw.PlanDriverPreview(ctx, "", previewOf("boreal-edge-web", map[string]string{"host": "10.20.4.31", "login": "display-login"}))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.NodeName != "edge-p" || plan.Driver != "boreal-edge-web" || plan.Target != "10.20.4.31:80" || len(plan.Tasks) != 1 || plan.Polls[0] != "identity" {
		t.Fatalf("plan = %+v", plan)
	}
	task := plan.Tasks[0]
	if task.Transport != "http" || task.Mode != "poll" || task.Session == nil || task.EndpointID != "" {
		t.Errorf("task = %+v", task)
	}
	if task.Secrets["login"]["password"] != previewPassword || len(plan.Sealed) != 1 || plan.Sealed[0] != previewPassword {
		t.Errorf("secrets = %v, sealed = %v", task.Secrets, plan.Sealed)
	}
	var params map[string]string
	if err := json.Unmarshal(task.EndpointParams, &params); err != nil || params["target"] != "10.20.4.31:80" {
		t.Errorf("params = %s (%v)", task.EndpointParams, err)
	}
	nodeRows, _ := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: "node", Verb: "preview"})
	secretRows, _ := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: "secret", Verb: "preview"})
	if len(nodeRows) != 1 || !strings.Contains(string(nodeRows[0].New), "10.20.4.31:80") || len(secretRows) != 1 {
		t.Errorf("audit: node %+v, secret %+v; want one preview row each", nodeRows, secretRows)
	}
	for _, r := range append(nodeRows, secretRows...) {
		if strings.Contains(string(r.New), previewPassword) {
			t.Errorf("an audit row carries the password: %s", r.New)
		}
	}
}

// What a preview cannot honestly run, or may not be given, is refused before a
// node is asked: a spec that does not validate (with its findings), a poll
// riding a held session, a poll the spec does not have, and a secret the
// caller may read but not reveal (a preview hands it to the target named).
func TestPlanDriverPreviewRefuses(t *testing.T) {
	gw, ctx := runStack(t)
	inputs := map[string]string{"host": "10.20.4.31", "login": "display-login"}

	in := previewOf("", inputs)
	in.Spec = []byte(`{"version":1,"transport":"http","polls":[{"name":"p","schedule":{"every":"soon"},"request":{"path":"/x"},"emits":[{"name":"uptime","extract":{"jsonpath":"a"}}]}]}`)
	var findings *storage.SpecFindingsError
	if _, err := gw.PlanDriverPreview(ctx, "", in); !errors.As(err, &findings) || len(findings.Findings) == 0 || !errors.Is(err, storage.ErrSpecInvalid) {
		t.Errorf("an invalid inline spec = %v, want its findings", err)
	}

	if _, err := gw.PlanDriverPreview(ctx, "", previewOf("newtron-nvp-telnet", map[string]string{"host": "10.20.4.40", "login": "display-login"})); !errors.Is(err, storage.ErrPreviewUnsupported) {
		t.Errorf("a held-session poll = %v, want ErrPreviewUnsupported", err)
	}

	in = previewOf("boreal-edge-web", inputs)
	in.Polls = []string{"nope"}
	if _, err := gw.PlanDriverPreview(ctx, "", in); !errors.Is(err, storage.ErrNoSuchPoll) || !strings.Contains(err.Error(), "identity") {
		t.Errorf("an unknown poll = %v, want ErrNoSuchPoll naming the polls there are", err)
	}

	in = previewOf("boreal-edge-web", inputs)
	in.SecretReveal = scope.Set{}
	if _, err := gw.PlanDriverPreview(ctx, "", in); !errors.Is(err, storage.ErrAttachInvalid) {
		t.Errorf("a secret the caller may not reveal = %v, want a refusal", err)
	}

	in = previewOf("boreal-edge-web", inputs)
	in.Node = "no-such-node"
	if _, err := gw.PlanDriverPreview(ctx, "", in); !errors.Is(err, storage.ErrNodeNotFound) {
		t.Errorf("an unknown node = %v, want ErrNodeNotFound", err)
	}
	if rows, _ := gw.ListAuditLog(ctx, storage.AuditFilter{Verb: "preview"}); len(rows) != 0 {
		t.Errorf("refused previews wrote audit rows: %+v", rows)
	}
}

// A run now resolves the endpoint's own poll task, on the node that holds it,
// and audits the asking; a poll the endpoint does not have, or an endpoint no
// node holds, is refused naming why.
func TestPlanEndpointRun(t *testing.T) {
	gw, ctx := runStack(t)
	all := scope.Set{All: true}
	comp, node, drv := "display-p", "edge-p", "boreal-edge-web"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Driver: &drv, Component: &comp, Node: &node,
		Inputs: map[string]string{"host": "10.20.4.31", "login": "display-login"}, SecretRead: all, CanAdmin: true}, all)
	if err != nil {
		t.Fatal(err)
	}
	run, err := gw.PlanEndpointRun(ctx, "", comp+"/"+ep.Name, "identity", all, all)
	if err != nil {
		t.Fatalf("plan run: %v", err)
	}
	tasks, err := gw.ListTasks(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, task := range tasks {
		if task.ID == run.TaskID && task.Mode == "poll" {
			found = true
		}
	}
	if !found || run.NodeName != node || run.Poll != "identity" || run.Component != comp {
		t.Errorf("run = %+v (task found %v)", run, found)
	}
	if rows, _ := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: "endpoint", Verb: "run"}); len(rows) != 1 || rows[0].ResourceID != ep.ID {
		t.Errorf("run audit rows = %+v", rows)
	}

	if _, err := gw.PlanEndpointRun(ctx, "", comp+"/"+ep.Name, "nope", all, all); !errors.Is(err, storage.ErrNoSuchPoll) || !strings.Contains(err.Error(), "identity") {
		t.Errorf("an unknown poll = %v, want ErrNoSuchPoll naming identity", err)
	}
	if _, err := gw.PlanEndpointRun(ctx, "", comp+"/"+ep.Name, "identity", scope.Set{}, scope.Set{}); err == nil {
		t.Error("an out-of-scope endpoint resolved")
	}
	unplaced := "display-q"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: unplaced}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	ep2, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Driver: &drv, Component: &unplaced,
		Inputs: map[string]string{"host": "10.20.4.32", "login": "display-login"}, SecretRead: all, CanAdmin: true}, all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.PlanEndpointRun(ctx, "", unplaced+"/"+ep2.Name, "identity", all, all); !errors.Is(err, storage.ErrEndpointUnplaced) {
		t.Errorf("an unplaced endpoint = %v, want ErrEndpointUnplaced", err)
	}
}
