// Package chart_test renders the Helm chart with Helm's own engine and asserts
// on the manifests it emits, so a chart change is tested like any other code.
package chart_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

const (
	release    = "og"
	dataDir    = "/var/lib/omniglass"
	keepPolicy = "helm.sh/resource-policy"
)

// render renders the chart as `helm install og` would with the given values
// overlaid, and returns the manifests by template file name.
func render(t *testing.T, vals map[string]any) (map[string]string, error) {
	t.Helper()
	chrt, err := loader.Load(".")
	if err != nil {
		t.Fatalf("load chart: %v", err)
	}
	rv, err := chartutil.ToRenderValues(chrt, vals,
		chartutil.ReleaseOptions{Name: release, Namespace: "omniglass", IsInstall: true, Revision: 1}, nil)
	if err != nil {
		t.Fatalf("render values: %v", err)
	}
	out, err := engine.Render(chrt, rv)
	if err != nil {
		return nil, err
	}
	byFile := map[string]string{}
	for name, body := range out {
		byFile[name[strings.LastIndex(name, "/")+1:]] = body
	}
	return byFile, nil
}

func mustRender(t *testing.T, vals map[string]any) map[string]string {
	t.Helper()
	out, err := render(t, vals)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return out
}

func decode(t *testing.T, manifest string, into any) {
	t.Helper()
	if strings.TrimSpace(manifest) == "" {
		t.Fatalf("manifest is empty, want a resource")
	}
	if err := yaml.Unmarshal([]byte(manifest), into); err != nil {
		t.Fatalf("decode manifest: %v\n%s", err, manifest)
	}
}

func deployment(t *testing.T, out map[string]string) appsv1.Deployment {
	t.Helper()
	var d appsv1.Deployment
	decode(t, out["deployment.yaml"], &d)
	return d
}

// container finds a container (or init container) by name.
func container(t *testing.T, d appsv1.Deployment, name string) corev1.Container {
	t.Helper()
	spec := d.Spec.Template.Spec
	for _, c := range append(spec.InitContainers, spec.Containers...) {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no container %q in the deployment", name)
	return corev1.Container{}
}

func envVar(c corev1.Container, name string) (corev1.EnvVar, bool) {
	for _, e := range c.Env {
		if e.Name == name {
			return e, true
		}
	}
	return corev1.EnvVar{}, false
}

// assertKeyFrom asserts the container reads OMNIGLASS_ENCRYPTION_KEY from the
// named Secret's encryption-key, never a literal.
func assertKeyFrom(t *testing.T, c corev1.Container, secretName string) {
	t.Helper()
	e, ok := envVar(c, "OMNIGLASS_ENCRYPTION_KEY")
	if !ok {
		t.Fatalf("%s: no OMNIGLASS_ENCRYPTION_KEY", c.Name)
	}
	if e.Value != "" || e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("%s: OMNIGLASS_ENCRYPTION_KEY = %+v, want a secretKeyRef", c.Name, e)
	}
	ref := e.ValueFrom.SecretKeyRef
	if ref.Name != secretName || ref.Key != "encryption-key" {
		t.Errorf("%s: key from %s/%s, want %s/encryption-key", c.Name, ref.Name, ref.Key, secretName)
	}
}

// TestDefaultsKeepTheKeyAndTheData is the durable-state contract (#1286): by
// default the chart generates a kept encryption-key Secret and a kept PVC,
// mounts the PVC at the image's data directory, and points the server's data
// directory and JetStream store into it.
func TestDefaultsKeepTheKeyAndTheData(t *testing.T) {
	out := mustRender(t, nil)

	var sec corev1.Secret
	decode(t, out["encryption-key-secret.yaml"], &sec)
	if sec.Name != release+"-encryption-key" {
		t.Errorf("key secret name = %q, want %s-encryption-key", sec.Name, release)
	}
	if sec.Annotations[keepPolicy] != "keep" {
		t.Errorf("key secret annotations = %v, want %s: keep", sec.Annotations, keepPolicy)
	}
	// The value is the server's key format: base64 that decodes to 32 bytes.
	raw, err := base64.StdEncoding.DecodeString(string(sec.Data["encryption-key"]))
	if err != nil || len(raw) != 32 {
		t.Errorf("generated key %q decodes to %d bytes (err %v), want 32", sec.Data["encryption-key"], len(raw), err)
	}

	var pvc corev1.PersistentVolumeClaim
	decode(t, out["pvc.yaml"], &pvc)
	if pvc.Annotations[keepPolicy] != "keep" {
		t.Errorf("pvc annotations = %v, want %s: keep", pvc.Annotations, keepPolicy)
	}
	if pvc.Spec.StorageClassName != nil {
		t.Errorf("pvc storageClassName = %q, want unset (the cluster default)", *pvc.Spec.StorageClassName)
	}

	d := deployment(t, out)
	if sc := d.Spec.Template.Spec.SecurityContext; sc == nil || sc.FSGroup == nil || *sc.FSGroup != 65532 {
		t.Errorf("pod securityContext = %+v, want fsGroup 65532 (the image's nonroot user) so the volume is writable", sc)
	}
	if d.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Errorf("strategy = %q, want Recreate (one server, one bus, one volume)", d.Spec.Strategy.Type)
	}
	var claim string
	for _, v := range d.Spec.Template.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			claim = v.Name
			if v.PersistentVolumeClaim.ClaimName != pvc.Name {
				t.Errorf("volume %s claims %q, want %q", v.Name, v.PersistentVolumeClaim.ClaimName, pvc.Name)
			}
		}
	}
	if claim == "" {
		t.Fatalf("deployment mounts no PVC; volumes = %+v", d.Spec.Template.Spec.Volumes)
	}
	server := container(t, d, "server")
	mounted := false
	for _, m := range server.VolumeMounts {
		if m.Name == claim && m.MountPath == dataDir {
			mounted = true
		}
	}
	if !mounted {
		t.Errorf("server mounts = %+v, want %s at %s", server.VolumeMounts, claim, dataDir)
	}
	for name, want := range map[string]string{
		"OMNIGLASS_DATA_DIR":       dataDir,
		"OMNIGLASS_NATS_STORE_DIR": dataDir + "/nats",
	} {
		if e, _ := envVar(server, name); e.Value != want {
			t.Errorf("server %s = %q, want %q", name, e.Value, want)
		}
	}
	assertKeyFrom(t, server, sec.Name)
	assertKeyFrom(t, container(t, d, "bootstrap"), sec.Name)
}

// A supplied Secret always wins: no Secret is generated and both containers
// read the operator's.
func TestExistingSecretWinsOverGeneration(t *testing.T) {
	out := mustRender(t, map[string]any{
		"encryptionKey": map[string]any{"existingSecret": "operator-kek"},
	})
	if body := strings.TrimSpace(out["encryption-key-secret.yaml"]); body != "" {
		t.Errorf("rendered a key secret alongside existingSecret:\n%s", body)
	}
	d := deployment(t, out)
	assertKeyFrom(t, container(t, d, "server"), "operator-kek")
	assertKeyFrom(t, container(t, d, "bootstrap"), "operator-kek")
}

// No supplied Secret and generation off is a chart that would boot with no
// key: it refuses to render, naming the values to set.
func TestNoKeySourceRefusesToRender(t *testing.T) {
	_, err := render(t, map[string]any{
		"encryptionKey": map[string]any{"generate": false},
	})
	if err == nil {
		t.Fatal("rendered with no encryption key source, want a refusal")
	}
	if !strings.Contains(err.Error(), "encryptionKey.existingSecret") {
		t.Errorf("error = %v, want it to name encryptionKey.existingSecret", err)
	}
}

// persistence.enabled=false keeps the data directory on an emptyDir and
// renders no PVC (a disposable preview).
func TestPersistenceOffUsesEmptyDir(t *testing.T) {
	out := mustRender(t, map[string]any{
		"persistence": map[string]any{"enabled": false},
	})
	if body := strings.TrimSpace(out["pvc.yaml"]); body != "" {
		t.Errorf("rendered a PVC with persistence off:\n%s", body)
	}
	d := deployment(t, out)
	for _, v := range d.Spec.Template.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			t.Errorf("volume %s claims a PVC with persistence off", v.Name)
		}
	}
	server := container(t, d, "server")
	if e, _ := envVar(server, "OMNIGLASS_NATS_STORE_DIR"); e.Value != dataDir+"/nats" {
		t.Errorf("OMNIGLASS_NATS_STORE_DIR = %q, want %s/nats", e.Value, dataDir)
	}
}

// persistence.storageClass and size reach the claim.
func TestPersistenceStorageClassAndSize(t *testing.T) {
	out := mustRender(t, map[string]any{
		"persistence": map[string]any{"storageClass": "fast", "size": "5Gi"},
	})
	var pvc corev1.PersistentVolumeClaim
	decode(t, out["pvc.yaml"], &pvc)
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "fast" {
		t.Errorf("storageClassName = %v, want fast", pvc.Spec.StorageClassName)
	}
	if got := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; got.String() != "5Gi" {
		t.Errorf("storage request = %s, want 5Gi", got.String())
	}
}

// NOTES.txt says loudly that the key must be backed up and prints the command
// that reads it out of whichever Secret holds it.
func TestNotesPrintTheKeyBackupCommand(t *testing.T) {
	for _, tc := range []struct {
		name   string
		vals   map[string]any
		secret string
	}{
		{"generated", nil, release + "-encryption-key"},
		{"existing", map[string]any{"encryptionKey": map[string]any{"existingSecret": "operator-kek"}}, "operator-kek"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notes := mustRender(t, tc.vals)["NOTES.txt"]
			if !strings.Contains(notes, "BACK UP THE ENCRYPTION KEY") {
				t.Errorf("NOTES.txt has no backup instruction:\n%s", notes)
			}
			cmd := "kubectl -n omniglass get secret " + tc.secret + " -o jsonpath='{.data.encryption-key}' | base64 -d"
			if !strings.Contains(notes, cmd) {
				t.Errorf("NOTES.txt does not print %q:\n%s", cmd, notes)
			}
		})
	}
}
