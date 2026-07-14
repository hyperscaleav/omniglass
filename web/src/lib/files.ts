import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The files data layer: thin typed wrappers over the generated client. A file is
// a tenant-wide, content-addressed blob with a searchable handle (name, MIME
// type, size, hash, sensitivity). Files are a flat list, not scoped or nested;
// the handle is addressed by its id. Bytes ride in and out base64-encoded (create
// takes an upload, download returns the blob), so this layer is pure I/O over the
// generated client and unit-testable against a mocked fetch.

export type FileRow = components["schemas"]["FileBody"];
// File is the same shape under the domain name; import FileRow at call sites that
// also touch the DOM File API, so the global File type is not shadowed.
export type File = FileRow;

export const FILES_KEY = ["files"] as const;

export type CreateFile = {
  name: string;
  contentType: string;
  content: string; // the file bytes, base64-encoded
  sensitive?: boolean;
};

// FileDownload is the payload of a download: the blob's bytes (base64) plus the
// name and MIME type to serve them under.
export type FileDownload = {
  name: string;
  content_type: string;
  content: string;
};

export async function listFiles(): Promise<FileRow[]> {
  const { data, error } = await api.GET("/files");
  if (error) throw error;
  return (data?.files ?? []) as FileRow[];
}

export async function getFile(id: string): Promise<FileRow> {
  const { data, error } = await api.GET("/files/{id}", { params: { path: { id } } });
  if (error) throw error;
  return data as FileRow;
}

// createFile stores an uploaded blob and its handle. Identical bytes dedup to one
// blob server-side; a sensitive file additionally requires the admin tier.
export async function createFile(input: CreateFile): Promise<FileRow> {
  const { data, error } = await api.POST("/files", {
    body: {
      name: input.name,
      content_type: input.contentType,
      content: input.content,
      sensitive: input.sensitive ?? false,
    },
  });
  if (error) throw error;
  return data as FileRow;
}

// downloadFile reads a file's bytes back (base64), the hash verified server-side.
export async function downloadFile(id: string): Promise<FileDownload> {
  const { data, error } = await api.GET("/files/{id}:download", { params: { path: { id } } });
  if (error) throw error;
  return data as FileDownload;
}

export async function deleteFile(id: string): Promise<void> {
  const { error } = await api.DELETE("/files/{id}", { params: { path: { id } } });
  if (error) throw error;
}

// humanSize renders a byte count for a table cell: bytes under 1 KiB verbatim,
// larger sizes to one decimal in the largest unit that keeps the number under
// 1024 (1200 -> "1.2 KB"). Uses 1024 steps with the conventional KB/MB labels.
export function humanSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "—";
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB", "PB"];
  let n = bytes / 1024;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n.toFixed(1)} ${units[i]}`;
}

// dataUrlToBase64 strips the "data:<mime>;base64," prefix a FileReader
// readAsDataURL result carries, leaving the raw base64 payload the API wants. A
// string with no prefix is returned unchanged.
export function dataUrlToBase64(dataUrl: string): string {
  const comma = dataUrl.indexOf(",");
  return comma >= 0 ? dataUrl.slice(comma + 1) : dataUrl;
}
