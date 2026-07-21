// Package migrate applies the embedded dbmate migration set. It is extracted
// from the CLI so tests (and the server boot path) can migrate an ephemeral
// database without going through the cobra layer.
package migrate

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/amacneil/dbmate/v2/pkg/dbmate"
	_ "github.com/amacneil/dbmate/v2/pkg/driver/postgres"
	"github.com/hyperscaleav/omniglass/db"
)

// Run applies all pending migrations against dsn.
func Run(dsn string) error {
	return withDBMate(dsn, func(mate *dbmate.DB) error { return mate.Migrate() })
}

// RollbackAll rolls every applied migration back, leaving an empty schema. It
// loops until dbmate reports nothing left (ErrNoRollback), so it needs no step
// count and stays correct as migrations are added. The production binary never
// rolls back; this exists so the down migrations get round-trip test coverage,
// since a down that fails (bad SQL) is a real bug the forward-only path cannot
// catch.
func RollbackAll(dsn string) error {
	return withDBMate(dsn, func(mate *dbmate.DB) error {
		for {
			if err := mate.Rollback(); err != nil {
				if errors.Is(err, dbmate.ErrNoRollback) {
					return nil
				}
				return err
			}
		}
	})
}

// RollbackOne rolls the single most recently applied migration back, leaving the
// database at the schema immediately before it. The production binary never rolls
// back; this exists so a test can stand a database up at the previous schema,
// write rows the way the old shape allowed, and then migrate forward over them,
// which is the only honest way to assert an upgrade preserves behavior.
func RollbackOne(dsn string) error {
	return withDBMate(dsn, func(mate *dbmate.DB) error { return mate.Rollback() })
}

func withDBMate(dsn string, fn func(*dbmate.DB) error) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("migrate: parse dsn: %w", err)
	}
	mate := dbmate.New(u)
	mate.FS = db.FS
	mate.MigrationsDir = []string{"migrations"}
	mate.AutoDumpSchema = false
	if err := fn(mate); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
