package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SeverityLevel is one named level of the severity registry (#917,
// ADR-0050): an alarm and a rule name it, the console renders its label and
// color, and its rank compares it to the others (higher is more severe).
// Official levels ship with the boot seed and are read-only.
type SeverityLevel struct {
	ID        string
	Name      string
	Label     string
	Color     string
	Rank      int
	Official  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SeverityLevelPatch carries an update's fields; nil is unchanged. A rename
// carries every alarm (and rule) naming the level with it.
type SeverityLevelPatch struct {
	Name  *string
	Label *string
	Color *string
	Rank  *int
}

// Severity level sentinels.
var (
	ErrSeverityLevelNotFound = errors.New("storage: severity level not found")
	ErrSeverityLevelExists   = errors.New("storage: a severity level already has that name or rank")
	ErrSeverityLevelOfficial = errors.New("storage: an official severity level is read-only")
	ErrSeverityLevelInvalid  = errors.New("storage: severity level is invalid")
	ErrSeverityLevelInUse    = errors.New("storage: severity level is in use")
)

// SeverityColors is the palette a level's color is drawn from: the console
// maps each to a hue, so a level cannot name a color nothing renders.
var SeverityColors = map[string]bool{"gray": true, "blue": true, "green": true, "yellow": true, "orange": true, "red": true, "darkred": true}

func validateSeverityLevel(name, color string, rank int) error {
	if err := ValidateName("severity_level", name); err != nil {
		return fmt.Errorf("%w: %w", ErrSeverityLevelInvalid, err)
	}
	if !SeverityColors[color] {
		return fmt.Errorf("%w: color %q is not one of gray, blue, green, yellow, orange, red, darkred", ErrSeverityLevelInvalid, color)
	}
	if rank <= 0 {
		return fmt.Errorf("%w: rank must be positive", ErrSeverityLevelInvalid)
	}
	return nil
}

const severityLevelCols = `id, name, coalesce(label, ''), color, rank, official, created_at, updated_at`

func scanSeverityLevel(row pgx.Row) (SeverityLevel, error) {
	var l SeverityLevel
	err := row.Scan(&l.ID, &l.Name, &l.Label, &l.Color, &l.Rank, &l.Official, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}

// UpsertSeverityLevel installs an official level, authoritative on conflict
// (the boot-seed bucket); an operator's own levels keep distinct names and
// are untouched.
func (p *PG) UpsertSeverityLevel(ctx context.Context, l SeverityLevel) error {
	if err := validateSeverityLevel(l.Name, l.Color, l.Rank); err != nil {
		return fmt.Errorf("storage: upsert severity level %q: %w", l.Name, err)
	}
	if _, err := p.pool.Exec(ctx, `insert into severity_level (name, label, color, rank, official)
		values ($1, $2, $3, $4, $5)
		on conflict (name) do update set label = excluded.label, color = excluded.color,
			rank = excluded.rank, official = excluded.official, updated_at = now()`,
		l.Name, labelOrNull(l.Label), l.Color, l.Rank, l.Official); err != nil {
		return fmt.Errorf("storage: upsert severity level %q: %w", l.Name, err)
	}
	return nil
}

// ListSeverityLevels lists the levels from least to most severe.
func (p *PG) ListSeverityLevels(ctx context.Context) ([]SeverityLevel, error) {
	rows, err := p.pool.Query(ctx, `select `+severityLevelCols+` from severity_level order by rank`)
	if err != nil {
		return nil, fmt.Errorf("storage: list severity levels: %w", err)
	}
	defer rows.Close()
	var out []SeverityLevel
	for rows.Next() {
		l, err := scanSeverityLevel(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan severity level: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetSeverityLevel reads one level by name.
func (p *PG) GetSeverityLevel(ctx context.Context, name string) (*SeverityLevel, error) {
	l, err := scanSeverityLevel(p.pool.QueryRow(ctx, `select `+severityLevelCols+` from severity_level where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSeverityLevelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get severity level %q: %w", name, err)
	}
	return &l, nil
}

// CreateSeverityLevel adds an operator level, audited.
func (p *PG) CreateSeverityLevel(ctx context.Context, actorID string, l SeverityLevel) (*SeverityLevel, error) {
	if err := validateSeverityLevel(l.Name, l.Color, l.Rank); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create severity level: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	created, err := scanSeverityLevel(tx.QueryRow(ctx, `insert into severity_level (name, label, color, rank, official)
		values ($1, $2, $3, $4, false) returning `+severityLevelCols, l.Name, labelOrNull(l.Label), l.Color, l.Rank))
	if isUniqueViolation(err) {
		return nil, ErrSeverityLevelExists
	}
	if err != nil {
		return nil, fmt.Errorf("storage: insert severity level %q: %w", l.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "severity_level", created.ID, nil, created); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create severity level: %w", err)
	}
	return &created, nil
}

// lockMutableSeverityLevel reads a level for change under its row lock,
// refusing an official one.
func lockMutableSeverityLevel(ctx context.Context, tx pgx.Tx, name string) (SeverityLevel, error) {
	l, err := scanSeverityLevel(tx.QueryRow(ctx, `select `+severityLevelCols+` from severity_level where name = $1 for update`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrSeverityLevelNotFound
	}
	if err != nil {
		return l, fmt.Errorf("storage: read severity level %q: %w", name, err)
	}
	if l.Official {
		return l, ErrSeverityLevelOfficial
	}
	return l, nil
}

// UpdateSeverityLevel patches an operator level, audited. A rename cascades
// to every alarm naming it.
func (p *PG) UpdateSeverityLevel(ctx context.Context, actorID, name string, patch SeverityLevelPatch) (*SeverityLevel, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update severity level: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, err := lockMutableSeverityLevel(ctx, tx, name)
	if err != nil {
		return nil, err
	}
	next := old
	if patch.Name != nil {
		next.Name = *patch.Name
	}
	if patch.Label != nil {
		next.Label = *patch.Label
	}
	if patch.Color != nil {
		next.Color = *patch.Color
	}
	if patch.Rank != nil {
		next.Rank = *patch.Rank
	}
	if err := validateSeverityLevel(next.Name, next.Color, next.Rank); err != nil {
		return nil, err
	}
	updated, err := scanSeverityLevel(tx.QueryRow(ctx, `update severity_level
		set name = $2, label = $3, color = $4, rank = $5, updated_at = now() where id = $1 returning `+severityLevelCols,
		old.ID, next.Name, labelOrNull(next.Label), next.Color, next.Rank))
	if isUniqueViolation(err) {
		return nil, ErrSeverityLevelExists
	}
	if err != nil {
		return nil, fmt.Errorf("storage: update severity level %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "severity_level", old.ID, old, updated); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update severity level: %w", err)
	}
	return &updated, nil
}

// DeleteSeverityLevel removes an operator level no alarm names, audited.
func (p *PG) DeleteSeverityLevel(ctx context.Context, actorID, name string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete severity level: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, err := lockMutableSeverityLevel(ctx, tx, name)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from severity_level where id = $1`, old.ID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrSeverityLevelInUse
		}
		return fmt.Errorf("storage: delete severity level %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "severity_level", old.ID, old, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
