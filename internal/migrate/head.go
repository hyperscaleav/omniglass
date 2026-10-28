package migrate

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"

	"github.com/amacneil/dbmate/v2/pkg/dbmate"
	"github.com/hyperscaleav/omniglass/db"
)

// ErrSchemaAhead is the refusal for a database a newer build migrated: its
// newest applied migration is one this binary does not embed.
var ErrSchemaAhead = errors.New("the database schema is newer than this binary")

// migrationFile is dbmate's own file grammar: the leading digits are the version.
var migrationFile = regexp.MustCompile(`^(\d+)_.*\.sql$`)

// checkHead refuses when the newest applied version is newer than the newest
// known one: an older image on a newer schema. An applied version below the
// known head that the binary no longer embeds (a chain collapsed by #388) is
// history, not a newer schema, so it passes. Versions are dbmate timestamps of
// one width, so string comparison orders them.
func checkHead(applied, known []string) error {
	appliedHead, knownHead := newest(applied), newest(known)
	if appliedHead <= knownHead {
		return nil
	}
	return fmt.Errorf("%w: its newest applied migration is %s and the newest this binary knows is %s; "+
		"run the release that migrated it, or a later one", ErrSchemaAhead, appliedHead, knownHead)
}

func newest(versions []string) string {
	head := ""
	for _, v := range versions {
		if v > head {
			head = v
		}
	}
	return head
}

// knownVersions lists the versions of the migrations embedded in the binary.
func knownVersions() ([]string, error) {
	entries, err := fs.ReadDir(db.FS, "migrations")
	if err != nil {
		return nil, err
	}
	var versions []string
	for _, e := range entries {
		if m := migrationFile.FindStringSubmatch(e.Name()); !e.IsDir() && m != nil {
			versions = append(versions, m[1])
		}
	}
	return versions, nil
}

// appliedVersions lists every version the database records as applied,
// including ones the binary does not embed, which dbmate's FindMigrations
// drops.
func appliedVersions(mate *dbmate.DB) ([]string, error) {
	drv, err := mate.Driver()
	if err != nil {
		return nil, err
	}
	sqlDB, err := drv.Open()
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()
	exists, err := drv.MigrationsTableExists(sqlDB)
	if err != nil || !exists {
		return nil, err
	}
	set, err := drv.SelectMigrations(sqlDB, -1)
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(set))
	for v := range set {
		versions = append(versions, v)
	}
	return versions, nil
}

// guardHead refuses a database a newer build migrated, before anything is applied.
func guardHead(mate *dbmate.DB) error {
	applied, err := appliedVersions(mate)
	if err != nil {
		return err
	}
	known, err := knownVersions()
	if err != nil {
		return err
	}
	return checkHead(applied, known)
}
