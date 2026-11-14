package main

import (
	"bytes"
	"os/exec"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHelmTemplateExtraEnv(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm binary not found")
	}

	chartDir := "."
	values := `server:
  extraEnv:
    - name: TEST_VAR
      value: test-value
    - name: TEST_SECRET
      valueFrom:
        secretKeyRef:
          name: test-secret
          key: test-key
  envFrom:
    - configMapRef:
        name: test-config
`

	cmd := exec.Command(helm, "template", "test-release", chartDir,
		"--values", "-",
	)
	cmd.Stdin = bytes.NewBufferString(values)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("helm template failed: %s\nstderr: %s", err, string(exitErr.Stderr))
		}
		t.Fatalf("helm template error: %v", err)
	}

	manifests := bytes.Split(output, []byte("---\n"))

	var deployment map[string]interface{}
	for _, manifest := range manifests {
		if len(bytes.TrimSpace(manifest)) == 0 {
			continue
		}

		var obj map[string]interface{}
		if err := yaml.Unmarshal(manifest, &obj); err != nil {
			continue
		}

		kind, ok := obj["kind"].(string)
		if !ok || kind != "Deployment" {
			continue
		}

		deployment = obj
		break
	}

	if deployment == nil {
		t.Fatal("no Deployment found in helm template output")
	}

	spec := getField(deployment, "spec")
	template := getField(spec, "template")
	podSpec := getField(template, "spec")
	containers := getSlice(podSpec, "containers")

	serverContainer := findContainerByName(containers, "server")
	if serverContainer == nil {
		t.Fatal("server container not found")
	}

	env := getSlice(serverContainer, "env")
	hasTestVar := findEnvVar(env, "TEST_VAR", "test-value")
	if !hasTestVar {
		t.Error("TEST_VAR not found in server container env")
	}

	hasTestSecret := findEnvVarWithValueFrom(env, "TEST_SECRET")
	if !hasTestSecret {
		t.Error("TEST_SECRET not found in server container env with valueFrom")
	}

	envFrom := getSlice(serverContainer, "envFrom")
	hasConfigMap := findEnvFromConfigMap(envFrom, "test-config")
	if !hasConfigMap {
		t.Error("configMapRef not found in server container envFrom")
	}

	initContainers := getSlice(podSpec, "initContainers")
	migrateContainer := findContainerByName(initContainers, "migrate")
	if migrateContainer == nil {
		t.Fatal("migrate init container not found")
	}

	migrateEnv := getSlice(migrateContainer, "env")
	hasMigrateTestVar := findEnvVar(migrateEnv, "TEST_VAR", "test-value")
	if !hasMigrateTestVar {
		t.Error("TEST_VAR not found in migrate init container env")
	}

	bootstrapContainer := findContainerByName(initContainers, "bootstrap")
	if bootstrapContainer != nil {
		bootstrapEnv := getSlice(bootstrapContainer, "env")
		hasBootstrapTestVar := findEnvVar(bootstrapEnv, "TEST_VAR", "test-value")
		if !hasBootstrapTestVar {
			t.Error("TEST_VAR not found in bootstrap init container env")
		}
	}
}

func getField(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	val, ok := m[key]
	if !ok {
		return nil
	}
	result, ok := val.(map[string]interface{})
	if !ok {
		return nil
	}
	return result
}

func getSlice(m map[string]interface{}, key string) []interface{} {
	if m == nil {
		return nil
	}
	val, ok := m[key]
	if !ok {
		return nil
	}
	result, ok := val.([]interface{})
	if !ok {
		return nil
	}
	return result
}

func findContainerByName(containers []interface{}, name string) map[string]interface{} {
	for _, c := range containers {
		container, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if n, ok := container["name"].(string); ok && n == name {
			return container
		}
	}
	return nil
}

func findEnvVar(env []interface{}, name, value string) bool {
	for _, e := range env {
		envVar, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		if n, ok := envVar["name"].(string); ok && n == name {
			if v, ok := envVar["value"].(string); ok && v == value {
				return true
			}
		}
	}
	return false
}

func findEnvVarWithValueFrom(env []interface{}, name string) bool {
	for _, e := range env {
		envVar, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		if n, ok := envVar["name"].(string); ok && n == name {
			if _, ok := envVar["valueFrom"]; ok {
				return true
			}
		}
	}
	return false
}

func findEnvFromConfigMap(envFrom []interface{}, name string) bool {
	for _, ef := range envFrom {
		envFromEntry, ok := ef.(map[string]interface{})
		if !ok {
			continue
		}
		if configMapRef, ok := envFromEntry["configMapRef"].(map[string]interface{}); ok {
			if n, ok := configMapRef["name"].(string); ok && n == name {
				return true
			}
		}
	}
	return false
}
