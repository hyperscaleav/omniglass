package storage

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// The lab mark (#1195). `omniglass lab` marks the database it creates and
// starts its server in lab mode; a lab and a real deployment then refuse each
// other's database at boot, so lab data never mixes with an operator's and a
// lab's well-known login never fronts real data.
var (
	// ErrLabDatabase is a server outside lab mode refusing a database the lab
	// marked.
	ErrLabDatabase = errors.New("this database belongs to an omniglass lab (it carries the lab mark), " +
		"and a server outside the lab refuses it: lab data is disposable and its login is well known. " +
		"Point OMNIGLASS_DSN at your own database, or run the lab with `omniglass lab run`")
	// ErrNotLabDatabase is a lab-mode server refusing a database that holds
	// data and no mark: a real deployment's.
	ErrNotLabDatabase = errors.New("this database holds data and has no lab mark, so a lab refuses it: " +
		"it looks like a real deployment's, and a lab would sign in to it with a well-known login. " +
		"A lab creates its own database; run `omniglass lab run --fresh` to start one over")
)

// LabState is what a server checks at boot: whether the lab marked the
// database, when, and whether it holds operator data (an owner exists).
type LabState struct {
	Marked    bool
	MarkedAt  time.Time
	HoldsData bool
}

// CheckLabMark is the boot decision, pure: a lab-mode server takes a marked
// database or an empty one (which the lab then marks), and a server outside
// lab mode takes any unmarked database.
func CheckLabMark(labMode, marked, holdsData bool) error {
	switch {
	case !labMode && marked:
		return ErrLabDatabase
	case labMode && !marked && holdsData:
		return ErrNotLabDatabase
	}
	return nil
}

// LabState reads the mark and whether the database holds data. It reads a
// schema of any age, so the lab's seed can judge a database BEFORE it
// migrates it: a table that is not there yet (no lab_mark, no human) reads as
// unmarked or empty, never as an error.
func (p *PG) LabState(ctx context.Context) (LabState, error) {
	var st LabState
	var hasMark, hasHuman bool
	if err := p.pool.QueryRow(ctx, `
		select to_regclass('lab_mark') is not null, to_regclass('human') is not null`).Scan(&hasMark, &hasHuman); err != nil {
		return LabState{}, fmt.Errorf("storage: lab state: %w", err)
	}
	if hasMark {
		var at *time.Time
		if err := p.pool.QueryRow(ctx, `select (select marked_at from lab_mark)`).Scan(&at); err != nil {
			return LabState{}, fmt.Errorf("storage: lab mark: %w", err)
		}
		if at != nil {
			st.Marked, st.MarkedAt = true, *at
		}
	}
	if hasHuman {
		if err := p.pool.QueryRow(ctx, `select exists(select 1 from human)`).Scan(&st.HoldsData); err != nil {
			return LabState{}, fmt.Errorf("storage: lab state: %w", err)
		}
	}
	return st, nil
}

// MarkLab writes the lab mark. Idempotent: an existing mark is kept.
func (p *PG) MarkLab(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `insert into lab_mark default values on conflict (singleton) do nothing`); err != nil {
		return fmt.Errorf("storage: mark lab: %w", err)
	}
	return nil
}
