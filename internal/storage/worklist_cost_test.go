package storage_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A node's worklist pull costs a number of statements that does not grow with
// the endpoints it carries (#1571). The pull ran a key-pin read per ssh or tls
// endpoint and a rejection read per component endpoint, so a 1,000-system
// corporate lab's headquarters node (806 components, 4,434 tasks) issued about
// 4,300 round trips per pull: past the node's 5 s pull timeout once the lab was
// loaded, a restart loop and a lab that never came up. Batching the pins and
// rejections left a secret resolve per (reference, component) pair, about 1,100
// statements, until the cascade resolved every pair in one statement (#1581).

// worklistProbe is a node with `endpoints` components, each carrying a tls
// endpoint with its own trusted key and its own rejection and an snmp driver
// endpoint whose community is a secret the component owns, and the worklist
// pulled once: the statements the pull issued, the worklist it returned, and
// the index of the component each endpoint id belongs to.
func worklistProbe(t *testing.T, endpoints int) (int, storage.Worklist, map[string]int) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, counter := storagetest.NewCountingDB(t, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	node, drv := "edge-w", "snmp-scalar"
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: node}, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: drv, Label: "SNMP Scalar", Version: "1.0.0", Spec: []byte(snmpSpec)}); err != nil {
		t.Fatal(err)
	}
	var writes []storage.PropertySampleWrite
	now := time.Now()
	owner := map[string]int{}
	for i := range endpoints {
		comp, cred := fmt.Sprintf("panel-w%d", i), fmt.Sprintf("cred-w%d", i)
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
		if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{Name: cred, SecretType: "snmp-community", OwnerKind: "component", OwnerName: &comp,
			Fields: map[string]string{"community": community(i)}}, all, true); err != nil {
			t.Fatal(err)
		}
		tlsEp, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tls", Component: &comp, Node: &node,
			Params: []byte(fmt.Sprintf(`{"target":"10.0.%d.%d:51326","tls-mode":"pin"}`, i/250, i%250+1))}, all)
		if err != nil {
			t.Fatal(err)
		}
		snmpEp, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Driver: &drv, Component: &comp, Node: &node,
			Inputs: map[string]string{"host": fmt.Sprintf("10.1.%d.%d", i/250, i%250+1), "community": cred}, SecretRead: all, CanAdmin: true}, all)
		if err != nil {
			t.Fatal(err)
		}
		owner[tlsEp.ID], owner[snmpEp.ID] = i, i
		writes = append(writes,
			storage.PropertySampleWrite{OwnerKind: "component", OwnerID: comp, Key: "tls-certificate", Instance: "tls", Value: pinOf(i), Source: "tls", TS: now},
			storage.PropertySampleWrite{OwnerKind: "component", OwnerID: comp, Key: "endpoint-rejection", Instance: "tls", Value: rejectionOf(i), Source: "tls", TS: now},
		)
	}
	if err := insertProperties(ctx, gw.(sampleSink), writes); err != nil {
		t.Fatal(err)
	}
	counter.Reset()
	wl, err := gw.NodeWorklist(ctx, node)
	if err != nil {
		t.Fatal(err)
	}
	return counter.N(), wl, owner
}

func community(i int) string   { return fmt.Sprintf("community-%d", i) }
func pinOf(i int) string       { return fmt.Sprintf("SHA256:%043d", i) }
func rejectionOf(i int) string { return fmt.Sprintf("rejected %d", i) }

func TestNodeWorklistStatementsDoNotGrowWithEndpoints(t *testing.T) {
	t.Parallel()
	small, _, _ := worklistProbe(t, 10)
	n, wl, owner := worklistProbe(t, 100)
	if n != small {
		t.Errorf("a worklist of 100 components cost %d statements, 10 cost %d: the pull must cost the same however many endpoints the node carries", n, small)
	}
	// The batched reads hand each endpoint its own pin, its own rejection and
	// its own component's secret, and nothing of another endpoint's.
	var tlsTasks, snmpTasks int
	for _, task := range wl.Tasks {
		i, ok := owner[task.EndpointID]
		if !ok {
			t.Fatalf("task %s rides endpoint %s, which the probe never created", task.ID, task.EndpointID)
		}
		switch task.Transport {
		case "tls":
			tlsTasks++
			if task.CertificatePin != pinOf(i) || task.Rejection != rejectionOf(i) {
				t.Errorf("tls task %s of panel-w%d carried pin %q and rejection %q, want %q and %q", task.ID, i, task.CertificatePin, task.Rejection, pinOf(i), rejectionOf(i))
			}
			if task.Secrets != nil {
				t.Errorf("tls task %s of panel-w%d carried secrets %v, want none", task.ID, i, task.Secrets)
			}
		case "snmp":
			snmpTasks++
			if got := task.Secrets["community"]["community"]; got != community(i) || len(task.Secrets) != 1 {
				t.Errorf("snmp task %s of panel-w%d carried secrets %v, want community %q alone", task.ID, i, task.Secrets, community(i))
			}
			if task.CertificatePin != "" || task.HostKey != "" || task.Rejection != "" {
				t.Errorf("snmp task %s of panel-w%d carried pin %q, host key %q, rejection %q, want none: they are the tls endpoint's", task.ID, i, task.CertificatePin, task.HostKey, task.Rejection)
			}
		}
	}
	if tlsTasks != 100 || snmpTasks < 100 {
		t.Errorf("worklist carried %d tls and %d snmp tasks, want 100 tls and at least 100 snmp", tlsTasks, snmpTasks)
	}
}
