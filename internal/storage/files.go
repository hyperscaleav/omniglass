package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// File sentinels. ErrFileNotFound is also the non-disclosing answer a sensitive
// file returns to a reader without the admin tier (a flagged file's existence is
// not disclosed), mirroring the secret surface.
var (
	ErrFileNotFound  = errors.New("storage: file not found")
	ErrFileForbidden = errors.New("storage: file forbidden")
	ErrFileInvalid   = errors.New("storage: file invalid")
)

// File is the searchable metadata handle over a blob. It owns no bytes; SHA256
// points at the blob. A file carries no estate placement: it is tenant-wide,
// gated by the file:<action> permission plus the sensitive tier.
type File struct {
	ID          string
	Name        string
	ContentType string
	Size        int64
	SHA256      string
	Sensitive   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// FileSpec is a create-from-upload request: the metadata plus the raw bytes the
// server hashes, dedups into a blob, and points the handle at.
type FileSpec struct {
	Name        string
	ContentType string
	Data        []byte
	Sensitive   bool
}

const fileCols = `id, name, content_type, size, sha256, sensitive, created_at, updated_at`

func scanFileRow(row pgx.Row) (*File, error) {
	var f File
	if err := row.Scan(&f.ID, &f.Name, &f.ContentType, &f.Size, &f.SHA256, &f.Sensitive, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, err
	}
	return &f, nil
}

func auditFile(f *File) map[string]any {
	return map[string]any{
		"name": f.Name, "content_type": f.ContentType,
		"size": f.Size, "sha256": f.SHA256, "sensitive": f.Sensitive,
	}
}

// ListFiles returns the file handles the caller may see, ordered by name. A
// sensitive file is dropped unless the caller holds the admin tier.
func (p *PG) ListFiles(ctx context.Context, canAdmin bool) ([]File, error) {
	rows, err := p.pool.Query(ctx, `select `+fileCols+` from file order by name, created_at`)
	if err != nil {
		return nil, fmt.Errorf("storage: list files: %w", err)
	}
	defer rows.Close()
	out := make([]File, 0)
	for rows.Next() {
		f, err := scanFileRow(rows)
		if err != nil {
			return nil, err
		}
		if f.Sensitive && !canAdmin {
			continue // a sensitive file is invisible without the admin tier
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// GetFile returns one file handle by id. A sensitive file is a non-disclosing
// ErrFileNotFound to a caller without the admin tier, and a malformed id is
// ErrFileNotFound (it cannot name an existing row).
func (p *PG) GetFile(ctx context.Context, id string, canAdmin bool) (*File, error) {
	f, err := scanFileRow(p.pool.QueryRow(ctx, `select `+fileCols+` from file where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) || isInvalidUUID(err) {
		return nil, ErrFileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get file: %w", err)
	}
	if f.Sensitive && !canAdmin {
		return nil, ErrFileNotFound
	}
	return f, nil
}

// CreateFile stores the uploaded bytes as a content-addressed blob (dedup on the
// hash), then writes the file handle pointing at it, audited in one transaction.
// A sensitive file may be created only by a caller holding the admin tier.
func (p *PG) CreateFile(ctx context.Context, actorID string, spec FileSpec, canAdmin bool) (*File, error) {
	if spec.Sensitive && !canAdmin {
		return nil, ErrFileForbidden
	}
	if err := file.Validate(spec.Name, spec.ContentType, spec.Data); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFileInvalid, err)
	}
	key, err := p.blob.Put(ctx, spec.Data)
	if err != nil {
		return nil, err
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create file: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	f, err := scanFileRow(tx.QueryRow(ctx, `
		insert into file (name, content_type, size, sha256, sensitive)
		values ($1, $2, $3, $4, $5)
		returning `+fileCols,
		spec.Name, spec.ContentType, len(spec.Data), key, spec.Sensitive))
	if err != nil {
		return nil, fmt.Errorf("storage: insert file: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "file", f.ID, nil, auditFile(f)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create file: %w", err)
	}
	return f, nil
}

// DownloadFile returns a file's metadata and its bytes, reading the blob the
// handle points at (the hash is verified on read). The same sensitive-tier gate
// as GetFile applies.
func (p *PG) DownloadFile(ctx context.Context, id string, canAdmin bool) (*File, []byte, error) {
	f, err := p.GetFile(ctx, id, canAdmin)
	if err != nil {
		return nil, nil, err
	}
	data, err := p.blob.Get(ctx, f.SHA256)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: download file: %w", err)
	}
	return f, data, nil
}

// DeleteFile removes a file handle, audited, and frees its blob when no other
// handle still references it (synchronous, dedup-aware reference counting, so
// deleting files reclaims storage rather than leaking it). Async mark-sweep GC
// of aged or event-referenced blobs is a separate later slice. The sensitive-tier
// gate applies: a caller without the admin tier gets the non-disclosing not-found.
func (p *PG) DeleteFile(ctx context.Context, actorID, id string, canAdmin bool) error {
	f, err := p.GetFile(ctx, id, canAdmin)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete file: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ct, err := tx.Exec(ctx, `delete from file where id = $1`, f.ID)
	if err != nil {
		return fmt.Errorf("storage: delete file: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrFileNotFound
	}
	// Free the blob only if the just-deleted handle was the last reference to it.
	// The handle row is already gone in this transaction, so the NOT EXISTS sees
	// only the surviving handles; a deduplicated blob shared by another file is
	// kept. One atomic statement, so a concurrent upload of the same bytes cannot
	// race the collect. (When other referencers land, large log bodies, a
	// collection.failed raw, an attach event, the general case moves to the async
	// mark-sweep GC slice.)
	if _, err := tx.Exec(ctx,
		`delete from blob where sha256 = $1 and not exists (select 1 from file where sha256 = $1)`,
		f.SHA256); err != nil {
		return fmt.Errorf("storage: free unreferenced blob: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "file", f.ID, auditFile(f), nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// isInvalidUUID reports whether err is Postgres's invalid-text-representation
// (22P02), which a malformed id triggers when cast to uuid. Such an id can never
// name an existing row, so the caller maps it to a non-disclosing not-found.
func isInvalidUUID(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
