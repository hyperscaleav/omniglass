package api

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// fileBody is the wire shape of a file handle: searchable metadata, no bytes.
type fileBody struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256" doc:"The content hash of the blob this handle points at"`
	Sensitive   bool   `json:"sensitive" doc:"When true, only the admin tier may see or download this file"`
	CreatedAt   string `json:"created_at"`
}

func toFileBody(f *storage.File) fileBody {
	return fileBody{
		ID: f.ID, Name: f.Name, ContentType: f.ContentType,
		Size: f.Size, SHA256: f.SHA256, Sensitive: f.Sensitive,
		CreatedAt: f.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type listFilesOutput struct {
	Body struct {
		Files []fileBody `json:"files"`
	}
}

type fileOutput struct {
	Body fileBody
}

type fileIDInput struct {
	ID string `path:"id" doc:"The file's id"`
}

type createFileInput struct {
	Body struct {
		Name        string `json:"name" minLength:"1" doc:"The file's display name (a label, no path separators)"`
		ContentType string `json:"content_type" minLength:"1" doc:"The MIME type used to serve the file"`
		Content     string `json:"content" doc:"The file bytes, base64-encoded"`
		Sensitive   *bool  `json:"sensitive,omitempty" doc:"Admin-only visibility; defaults false. Setting true requires the admin tier"`
	}
}

type downloadFileOutput struct {
	Body struct {
		Name        string `json:"name"`
		ContentType string `json:"content_type"`
		Content     string `json:"content" doc:"The file bytes, base64-encoded"`
	}
}

// canFileAdmin reports whether the caller holds the file action at the admin
// tier (file:<action>:admin), which admin and owner reach via file:> / >. It
// gates sensitive files: they are hidden from a lister without it and answered
// with a non-disclosing 404 to a reader without it, and only such a caller may
// create one.
func (a *authenticator) canFileAdmin(ctx context.Context, action string) bool {
	perms, ok := permsFrom(ctx)
	return ok && perms.Allows("file", action, "admin")
}

// registerFileRoutes wires the file surface: the tenant-wide directory, create
// from upload, metadata get, byte download, and delete. Reading rides the viewer
// floor (file:read, which the bare *:read carries since a file is not a sensitive
// resource); create and delete are gated by file:create / file:delete; the
// sensitive flag then fences individual rows to the admin tier.
func registerFileRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, huma.Operation{
		OperationID: "list-files",
		Method:      http.MethodGet,
		Path:        "/files",
		Summary:     "List files",
		Description: "Lists the file handles the caller may see (searchable metadata, no bytes). Sensitive files appear only to the admin tier. Gated by file:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("file", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listFilesOutput, error) {
		files, err := gw.ListFiles(ctx, a.canFileAdmin(ctx, "read"))
		if err != nil {
			return nil, mapFileErr(err)
		}
		out := &listFilesOutput{}
		out.Body.Files = make([]fileBody, 0, len(files))
		for i := range files {
			out.Body.Files = append(out.Body.Files, toFileBody(&files[i]))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-file",
		Method:        http.MethodPost,
		Path:          "/files",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a file from an upload",
		Description:   "Stores the uploaded bytes as a content-addressed blob (identical bytes dedup to one blob) and writes the file handle pointing at it. Gated by file:create; a sensitive file additionally needs the admin tier (file:create:admin).",
		Middlewares:   huma.Middlewares{a.authn, a.require("file", "create")},
	}, func(ctx context.Context, in *createFileInput) (*fileOutput, error) {
		data, err := base64.StdEncoding.DecodeString(in.Body.Content)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("content is not valid base64")
		}
		sensitive := false
		if in.Body.Sensitive != nil {
			sensitive = *in.Body.Sensitive
		}
		f, err := gw.CreateFile(ctx, actorID(ctx), storage.FileSpec{
			Name:        in.Body.Name,
			ContentType: in.Body.ContentType,
			Data:        data,
			Sensitive:   sensitive,
		}, a.canFileAdmin(ctx, "create"))
		if err != nil {
			return nil, mapFileErr(err)
		}
		return &fileOutput{Body: toFileBody(f)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-file",
		Method:      http.MethodGet,
		Path:        "/files/{id}",
		Summary:     "Get a file's metadata",
		Description: "Returns one file handle's searchable metadata (no bytes). A sensitive file is a non-disclosing 404 without the admin tier. Gated by file:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("file", "read")},
	}, func(ctx context.Context, in *fileIDInput) (*fileOutput, error) {
		f, err := gw.GetFile(ctx, in.ID, a.canFileAdmin(ctx, "read"))
		if err != nil {
			return nil, mapFileErr(err)
		}
		return &fileOutput{Body: toFileBody(f)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "download-file",
		Method:      http.MethodGet,
		Path:        "/files/{id}:download",
		Summary:     "Download a file's bytes",
		Description: "Returns a file's bytes (base64-encoded) read from the blob it points at, the hash verified on read. A sensitive file is a non-disclosing 404 without the admin tier. Gated by file:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("file", "read")},
	}, func(ctx context.Context, in *fileIDInput) (*downloadFileOutput, error) {
		f, data, err := gw.DownloadFile(ctx, in.ID, a.canFileAdmin(ctx, "read"))
		if err != nil {
			return nil, mapFileErr(err)
		}
		out := &downloadFileOutput{}
		out.Body.Name = f.Name
		out.Body.ContentType = f.ContentType
		out.Body.Content = base64.StdEncoding.EncodeToString(data)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-file",
		Method:        http.MethodDelete,
		Path:          "/files/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a file",
		Description:   "Removes a file handle. The underlying blob is left in place (garbage collection is a later slice). A sensitive file is a non-disclosing 404 without the admin tier. Gated by file:delete.",
		Middlewares:   huma.Middlewares{a.authn, a.require("file", "delete")},
	}, func(ctx context.Context, in *fileIDInput) (*struct{}, error) {
		if err := gw.DeleteFile(ctx, actorID(ctx), in.ID, a.canFileAdmin(ctx, "delete")); err != nil {
			return nil, mapFileErr(err)
		}
		return nil, nil
	})
}

// mapFileErr translates the gateway's file sentinels into HTTP status.
func mapFileErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrFileNotFound):
		return huma.Error404NotFound("file not found")
	case errors.Is(err, storage.ErrFileForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrFileInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		return huma.Error500InternalServerError("file operation failed")
	}
}
