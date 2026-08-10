package storage

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// audit_log.resource_id must be the entity's primary key. This guard reads every
// call site rather than sampling behaviour, because resource_id is a string
// parameter that no type can constrain, and because the first pass at this fix
// worked from a list of sites grepped by eye: the list missed property_type, vendor,
// and system_member, and a behavioural test covering three other resources went
// green over the gap.
//
// It is a WHITELIST, not a blacklist, and that is the load-bearing choice. A
// blacklist catches expressions that look like a name (`spec.Name`, `systemName`)
// and waves through the more common mistake, which is passing the route's `id`
// parameter: in a dual-accept gateway that variable holds a uuid or a name depending
// only on how the caller addressed the resource, so it reads as correct and is the
// exact bug this exists to stop. Anything not obviously a primary key has to be
// named below with a reason.

// primaryKeyShaped is what a resource_id may look like without an explicit excuse: a
// struct's ID field, or the helper that lifts an id out of a row image.
var primaryKeyShaped = regexp.MustCompile(`\.(ID|PrincipalID)$|^[a-z][A-Za-z0-9]*ID$|^auditImageID\(`)

// auditKeyAllowed names every resource_id expression that is not primary-key-shaped
// but is correct anyway, with the reason. An entry is a claim somebody made on
// purpose, which is the whole difference between an exception and an oversight.
var auditKeyAllowed = map[string]string{
	"namespace":        "settings is keyed by namespace: there is no surrogate id to use instead",
	"uid":              "the row's uuid, resolved by the caller before the delete",
	"pid":              "the principal uuid, resolved by the caller",
	"gid":              "the grant uuid returned by the insert",
	`fmt.Sprintf("%d"`: "command's primary key is a bigint, not a uuid",
	`""`:               "the settings delete-all leg addresses no single row",
	"kind":             "label_rule is keyed by entity kind: one row per labelled entity kind, no surrogate id",
}

func TestEveryAuditRowIsKeyedOnAPrimaryKey(t *testing.T) {
	// writeAuditRes(ctx, tx, actor, verb, resource, resourceID, old, new)
	call := regexp.MustCompile(`writeAuditRes\([^,]+,\s*[^,]+,\s*[^,]+,\s*([^,]+),\s*([^,]+),\s*([^,]+),`)

	var checked int
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range call.FindAllStringSubmatch(string(src), -1) {
			// writeAuditRes's own declaration matches too; it is the rule's subject,
			// not a use of it.
			if strings.Contains(m[0], "resourceID string") {
				continue
			}
			resource, id := strings.TrimSpace(m[2]), strings.TrimSpace(m[3])
			checked++
			if _, ok := auditKeyAllowed[id]; ok || primaryKeyShaped.MatchString(id) {
				continue
			}
			t.Errorf("%s keys a %s audit row on %q, which is not obviously a primary key.\n"+
				"resource_id must be the entity's primary key. A name is renameable, so a row "+
				"keyed on one is orphaned by the rename; and a dual-accept route's ref holds a "+
				"uuid or a name depending only on how the caller addressed the resource, so the "+
				"same entity ends up with two audit keys.\n"+
				"The name at the time of the action is not lost: it belongs in the old/new "+
				"images, as a snapshot rather than a lookup key.\n"+
				"If this expression really is the primary key, name it in auditKeyAllowed with "+
				"the reason.", path, resource, id)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if checked == 0 {
		t.Fatal("found no writeAuditRes call sites, so this guard is not reading what it thinks it is")
	}
	t.Logf("checked %d writeAuditRes call sites", checked)
}
