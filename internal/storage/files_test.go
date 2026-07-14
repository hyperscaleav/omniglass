package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/blob"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newFileGW builds a gateway and a blob store over the SAME fresh database, so a
// test can assert both the file handle and the underlying blob (e.g. that a
// delete leaves the blob).
func newFileGW(t *testing.T) (context.Context, storage.Gateway, blob.Store) {
	t.Helper()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPG: %v", err)
	}
	t.Cleanup(gw.Close)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	t.Cleanup(pool.Close)
	return ctx, gw, storage.NewPGBlobStore(pool)
}

func TestCreateFileStoresBlobAndDownloadsIdenticalBytes(t *testing.T) {
	ctx, gw, _ := newFileGW(t)
	payload := []byte("PK\x03\x04 firmware image bytes")

	f, err := gw.CreateFile(ctx, "", storage.FileSpec{
		Name: "codec-fw-2.1.bin", ContentType: "application/octet-stream", Data: payload,
	}, false)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if f.SHA256 != blob.Hash(payload) {
		t.Fatalf("file sha256 = %q, want the content hash", f.SHA256)
	}
	if f.Size != int64(len(payload)) {
		t.Fatalf("file size = %d, want %d", f.Size, len(payload))
	}

	meta, data, err := gw.DownloadFile(ctx, f.ID, false)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("download bytes = %q, want %q", data, payload)
	}
	if meta.ContentType != "application/octet-stream" {
		t.Fatalf("download content type = %q", meta.ContentType)
	}
}

func TestCreateFileDedupsIdenticalBytesToOneBlob(t *testing.T) {
	ctx, gw, _ := newFileGW(t)
	payload := []byte("identical runbook contents")

	a, err := gw.CreateFile(ctx, "", storage.FileSpec{Name: "a.txt", ContentType: "text/plain", Data: payload}, false)
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := gw.CreateFile(ctx, "", storage.FileSpec{Name: "b.txt", ContentType: "text/plain", Data: payload}, false)
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	if a.ID == b.ID {
		t.Fatal("two uploads collapsed into one file handle; want two handles")
	}
	if a.SHA256 != b.SHA256 {
		t.Fatalf("identical bytes got different blob keys: %q vs %q", a.SHA256, b.SHA256)
	}
}

func TestDeleteFileRemovesHandleButLeavesBlob(t *testing.T) {
	ctx, gw, bs := newFileGW(t)
	payload := []byte("a packet capture")

	f, err := gw.CreateFile(ctx, "", storage.FileSpec{Name: "cap.pcap", ContentType: "application/vnd.tcpdump.pcap", Data: payload}, false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := gw.DeleteFile(ctx, "", f.ID, false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := gw.GetFile(ctx, f.ID, false); !errors.Is(err, storage.ErrFileNotFound) {
		t.Fatalf("get after delete = %v, want ErrFileNotFound", err)
	}
	// GC is deferred: the delete releases the handle, not the blob.
	ok, err := bs.Exists(ctx, f.SHA256)
	if err != nil || !ok {
		t.Fatalf("blob after file delete: exists=%v err=%v; want retained", ok, err)
	}
}

func TestCreateFileRejectsMalformedUpload(t *testing.T) {
	ctx, gw, _ := newFileGW(t)
	_, err := gw.CreateFile(ctx, "", storage.FileSpec{Name: "a/b.txt", ContentType: "text/plain", Data: []byte("x")}, false)
	if !errors.Is(err, storage.ErrFileInvalid) {
		t.Fatalf("path-bearing name = %v, want ErrFileInvalid", err)
	}
}

// The sensitive flag mirrors secret.admin_sensitive: creating one needs the
// admin tier, and a flagged file is invisible / non-disclosing without it.
func TestSensitiveFileRequiresAdminToCreate(t *testing.T) {
	ctx, gw, _ := newFileGW(t)
	_, err := gw.CreateFile(ctx, "", storage.FileSpec{
		Name: "competitive-quote.pdf", ContentType: "application/pdf", Data: []byte("bid"), Sensitive: true,
	}, false)
	if !errors.Is(err, storage.ErrFileForbidden) {
		t.Fatalf("sensitive create without admin = %v, want ErrFileForbidden", err)
	}
}

func TestSensitiveFileHiddenFromNonAdminListAndGet(t *testing.T) {
	ctx, gw, _ := newFileGW(t)
	// admin creates one sensitive and one ordinary file.
	sens, err := gw.CreateFile(ctx, "", storage.FileSpec{Name: "quote.pdf", ContentType: "application/pdf", Data: []byte("bid"), Sensitive: true}, true)
	if err != nil {
		t.Fatalf("create sensitive: %v", err)
	}
	if _, err := gw.CreateFile(ctx, "", storage.FileSpec{Name: "runbook.md", ContentType: "text/markdown", Data: []byte("steps")}, false); err != nil {
		t.Fatalf("create ordinary: %v", err)
	}

	// A non-admin lister sees only the ordinary file.
	list, err := gw.ListFiles(ctx, false)
	if err != nil {
		t.Fatalf("list non-admin: %v", err)
	}
	for _, f := range list {
		if f.Sensitive {
			t.Fatalf("non-admin list surfaced a sensitive file %q", f.Name)
		}
	}
	if len(list) != 1 {
		t.Fatalf("non-admin list len = %d, want 1 (ordinary only)", len(list))
	}

	// A non-admin reader of the sensitive file gets a non-disclosing not-found.
	if _, err := gw.GetFile(ctx, sens.ID, false); !errors.Is(err, storage.ErrFileNotFound) {
		t.Fatalf("non-admin get sensitive = %v, want ErrFileNotFound", err)
	}
	// The admin tier sees it.
	if _, err := gw.GetFile(ctx, sens.ID, true); err != nil {
		t.Fatalf("admin get sensitive: %v", err)
	}
	// An admin lister sees both.
	adminList, err := gw.ListFiles(ctx, true)
	if err != nil {
		t.Fatalf("list admin: %v", err)
	}
	if len(adminList) != 2 {
		t.Fatalf("admin list len = %d, want 2", len(adminList))
	}
}
