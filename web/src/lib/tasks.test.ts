import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  listTasks,
  getTask,
  createTask,
  updateTask,
  deleteTask,
  taskLabel,
  taskFilterKeys,
  type Task,
} from "./tasks";
import { buildPredicate, type Chip } from "./predicate";

// The data layer is the unit under test; fetch is the seam we fake, so these assert
// the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("tasks data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists tasks and unwraps the envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ tasks: [{ id: "t-1", interface: "disp-1-tcp", mode: "poll", enabled: true }] }),
    );
    const tasks = await listTasks();
    expect(tasks).toHaveLength(1);
    expect(tasks[0].id).toBe("t-1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/tasks");
  });

  it("tolerates a null tasks envelope (none yet)", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ tasks: null }));
    expect(await listTasks()).toEqual([]);
  });

  it("gets a task by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "t-1", interface: "disp-1-tcp", mode: "poll", enabled: true }));
    const t = await getTask("t-1");
    expect(t.id).toBe("t-1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/tasks/t-1");
  });

  it("posts the create body (interface, mode, enabled) and returns the created task", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "t-9", interface: "disp-1-tcp", mode: "poll", enabled: true }, 201),
    );
    const created = await createTask({ interface: "disp-1-tcp", mode: "poll", enabled: true });
    expect(created.id).toBe("t-9");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/tasks");
    expect(await req.json()).toMatchObject({ interface: "disp-1-tcp", mode: "poll", enabled: true });
  });

  it("patches only the mutable fields (enabled, display_name, node) on update", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "t-1", interface: "disp-1-tcp", mode: "poll", enabled: false }));
    await updateTask("t-1", { enabled: false, display_name: "HQ ping" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain("/api/v1/tasks/t-1");
    expect(await req.json()).toEqual({ enabled: false, display_name: "HQ ping" });
  });

  it("deletes a task by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteTask("t-1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/tasks/t-1");
  });

  it("throws on an error status (e.g. a duplicate content-addressed task, 409)", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "task already exists" }, 409));
    await expect(createTask({ interface: "disp-1-tcp", mode: "poll" })).rejects.toBeTruthy();
  });
});

// taskLabel renders the display name when set, else the content-addressed id.
describe("taskLabel", () => {
  it("prefers the display name", () => {
    expect(taskLabel({ id: "t-1", interface: "i", mode: "poll", enabled: true, display_name: "HQ ping" })).toBe("HQ ping");
  });
  it("falls back to the id (the address)", () => {
    expect(taskLabel({ id: "t-1", interface: "i", mode: "poll", enabled: true })).toBe("t-1");
  });
});

// taskFilterKeys are the console's shared faceted search: interface (exact), mode
// (exact), and enabled (exact, over the boolean rendered as true/false). Matching is
// client-side via lib/predicate.
const tasks: Task[] = [
  { id: "t-1", interface: "disp-1-tcp", mode: "poll", enabled: true },
  { id: "t-2", interface: "disp-1-icmp", mode: "poll", enabled: false },
  { id: "t-3", interface: "sess-1", mode: "listen", enabled: true },
];
const matched = (chips: Chip[]): string[] => tasks.filter(buildPredicate(taskFilterKeys, chips)).map((t) => t.id);

describe("taskFilterKeys", () => {
  it("filters by interface (exact)", () => {
    expect(matched([{ key: "interface", op: "eq", values: ["disp-1-tcp"] }])).toEqual(["t-1"]);
  });
  it("filters by mode (exact)", () => {
    expect(matched([{ key: "mode", op: "eq", values: ["listen"] }])).toEqual(["t-3"]);
  });
  it("filters by enabled (exact, over the boolean)", () => {
    expect(matched([{ key: "enabled", op: "eq", values: ["true"] }])).toEqual(["t-1", "t-3"]);
    expect(matched([{ key: "enabled", op: "eq", values: ["false"] }])).toEqual(["t-2"]);
  });
  it("offers the enabled value catalog", () => {
    const byKey = Object.fromEntries(taskFilterKeys.map((k) => [k.key, k]));
    expect(byKey.enabled.values!(tasks)).toEqual(["false", "true"]);
  });
});
