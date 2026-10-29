package storage

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A driver function declaring no identity and no records bakes to the bytes
// it always did (#1386), so every derived task keeps its content hash and an
// upgrade re-derives nothing. The golden below was captured before records
// and identity existed.

const pinnedSpec = `{"version":1,"transport":"http","polls":[{"name":"status","schedule":{"every":"30s"},"request":{"path":"/api/status"},"emits":[{"name":"power-state","extract":{"jsonpath":"device.power"}},{"name":"volume","extract":{"jsonpath":"audio.volume"},"transform":{"scale":0.5}}]}]}`

const (
	pinnedTaskID   = "96b439864e2bf2cf9485382d36028a36a472738415812b22ec484d00be47146b"
	pinnedTaskSpec = `{"driver":"kestrel","version":"2","function":"status","schedule":{"every":"30s"},"request":{"path":"/api/status"},"emits":[{"name":"power-state","lane":"property","extract":{"jsonpath":"device.power"}},{"name":"volume","lane":"metric","extract":{"jsonpath":"audio.volume"},"transform":{"scale":0.5}}]}`
)

func pinnedCatalog() catalogMaps {
	return catalogMaps{lanes: map[string]string{"power-state": "property", "volume": "metric", "ucc-room-id": "property"}}
}

func derivePinned(t *testing.T, spec string) []derivedTask {
	t.Helper()
	sp, err := driver.Parse([]byte(spec))
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := derivedDriverTasks("ep-1", &Driver{Name: "kestrel", Version: "2"}, sp, pinnedCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("derived %d tasks, want 1", len(tasks))
	}
	return tasks
}

func TestAFunctionWithoutIdentityKeepsItsTaskHash(t *testing.T) {
	task := derivePinned(t, pinnedSpec)[0]
	if string(task.spec) != pinnedTaskSpec || task.id != pinnedTaskID {
		t.Fatalf("derived\n %s %s\nwant\n %s %s", task.id, task.spec, pinnedTaskID, pinnedTaskSpec)
	}
}

func TestAnIdentifiedFunctionBakesItsIdentityAndRecords(t *testing.T) {
	spec := strings.Replace(pinnedSpec, `"emits":`, `"records":"rooms","identity":{"key":"ucc-room-id","extract":{"jsonpath":"id"}},"emits":`, 1)
	task := derivePinned(t, spec)[0]
	fn, err := driver.ParseBaked(task.spec)
	if err != nil {
		t.Fatal(err)
	}
	if fn.Records != "rooms" || fn.Identity == nil || fn.Identity.Key != "ucc-room-id" || fn.Identity.Extract.JSONPath != "id" {
		t.Fatalf("baked %s, want its records and identity carried", task.spec)
	}
	if task.id == pinnedTaskID {
		t.Fatal("an identified function derived the unidentified one's task id")
	}
}
