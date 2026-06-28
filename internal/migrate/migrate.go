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
