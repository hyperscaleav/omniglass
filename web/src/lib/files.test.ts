import { describe, it, expect, vi, beforeEach } from "vitest";
import { listFiles, getFile, createFile, downloadFile, deleteFile, humanSize, dataUrlToBase64 } from "./files";

// The data layer is the unit under test; fetch is the seam we fake, so these
// assert the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

const sampleFile = {
  id: "file_1",
  name: "firmware.bin",
  content_type: "application/octet-stream",
  size: 2048,
  sha256: "abc123",
  sensitive: false,
  created_at: "2026-07-14T12:00:00Z",
};

describe("files data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists files and unwraps the envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ files: [sampleFile] }));
    const files = await listFiles();
    expect(files).toHaveLength(1);
    expect(files[0].name).toBe("firmware.bin");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/files");
  });

  it("returns an empty list when the envelope is null", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ files: null }));
    expect(await listFiles()).toEqual([]);
  });

  it("gets one file by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(sampleFile));
    const f = await getFile("file_1");
    expect(f.sha256).toBe("abc123");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/files/file_1");
  });

  it("posts the create body with base64 content, content_type, and sensitive", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ ...sampleFile, sensitive: true }, 201));
    const created = await createFile({ name: "key.pem", contentType: "application/x-pem-file", content: "aGVsbG8=", sensitive: true });
    expect(created.id).toBe("file_1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    const sent = await req.json();
    expect(sent).toMatchObject({
      name: "key.pem",
      content_type: "application/x-pem-file",
      content: "aGVsbG8=",
      sensitive: true,
    });
  });

  it("defaults sensitive to false when omitted", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(sampleFile, 201));
    await createFile({ name: "notes.txt", contentType: "text/plain", content: "eA==" });
    const req = fetchMock.mock.calls[0][0] as Request;
    const sent = await req.json();
    expect(sent.sensitive).toBe(false);
  });

  it("downloads a file's bytes, hitting the :download custom method", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ name: "firmware.bin", content_type: "application/octet-stream", content: "aGVsbG8=" }),
    );
    const dl = await downloadFile("file_1");
    expect(dl.content).toBe("aGVsbG8=");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/files/file_1:download");
  });

  it("deletes by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteFile("file_1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/files/file_1");
  });

  it("throws on an error status", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "sensitive files need the admin tier" }, 403));
    await expect(createFile({ name: "x", contentType: "text/plain", content: "eA==", sensitive: true })).rejects.toBeTruthy();
  });
});

describe("humanSize", () => {
  it("renders bytes under 1 KiB verbatim", () => {
    expect(humanSize(0)).toBe("0 B");
    expect(humanSize(512)).toBe("512 B");
    expect(humanSize(1023)).toBe("1023 B");
  });
  it("renders larger sizes to one decimal in the right unit", () => {
    expect(humanSize(1024)).toBe("1.0 KB");
    expect(humanSize(1200)).toBe("1.2 KB");
    expect(humanSize(1536)).toBe("1.5 KB");
    expect(humanSize(1048576)).toBe("1.0 MB");
    expect(humanSize(5_368_709_120)).toBe("5.0 GB");
  });
  it("guards a negative or non-finite count", () => {
    expect(humanSize(-1)).toBe("—");
    expect(humanSize(NaN)).toBe("—");
  });
});

describe("dataUrlToBase64", () => {
  it("strips a data-URL prefix", () => {
    expect(dataUrlToBase64("data:application/octet-stream;base64,aGVsbG8=")).toBe("aGVsbG8=");
    expect(dataUrlToBase64("data:text/plain;base64,eA==")).toBe("eA==");
  });
  it("returns a bare base64 string unchanged", () => {
    expect(dataUrlToBase64("aGVsbG8=")).toBe("aGVsbG8=");
  });
});
