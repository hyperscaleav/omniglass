import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  listTasks,
  getTask,
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
      jsonResponse({ tasks: [{ id: "t-1", interface_id: "if-tcp", mode: "poll", enabled: true }] }),
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
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "t-1", interface_id: "if-tcp", mode: "poll", enabled: true }));
    const t = await getTask("t-1");
    expect(t.id).toBe("t-1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/tasks/t-1");
  });
});

// taskLabel renders the display name when set, else the content-addressed id.
describe("taskLabel", () => {
  it("prefers the display name", () => {
    expect(taskLabel({ id: "t-1", interface_id: "if-i", mode: "poll", enabled: true, display_name: "HQ ping" })).toBe("HQ ping");
  });
  it("falls back to the id (the address)", () => {
    expect(taskLabel({ id: "t-1", interface_id: "if-i", mode: "poll", enabled: true })).toBe("t-1");
  });
});

// taskFilterKeys are the console's shared faceted search: interface (exact, resolved
// from interface_id to the friendly name via nameOf), mode (exact), and enabled
// (exact, over the boolean rendered as true/false). Matching is client-side via
// lib/predicate.
const tasks: Task[] = [
  { id: "t-1", interface_id: "if-tcp", mode: "poll", enabled: true },
  { id: "t-2", interface_id: "if-icmp", mode: "poll", enabled: false },
  { id: "t-3", interface_id: "if-sess", mode: "listen", enabled: true },
];
const IFACE_NAME: Record<string, string> = { "if-tcp": "disp-1-tcp", "if-icmp": "disp-1-icmp", "if-sess": "sess-1" };
const nameOf = (id: string): string => IFACE_NAME[id] ?? id;
const keys = taskFilterKeys(nameOf);
const matched = (chips: Chip[]): string[] => tasks.filter(buildPredicate(keys, chips)).map((t) => t.id);

describe("taskFilterKeys", () => {
  it("filters by interface (exact, over the resolved friendly name)", () => {
    expect(matched([{ key: "interface", op: "eq", values: ["disp-1-tcp"] }])).toEqual(["t-1"]);
  });
  it("filters by mode (exact)", () => {
    expect(matched([{ key: "mode", op: "eq", values: ["listen"] }])).toEqual(["t-3"]);
  });
  it("filters by enabled (exact, over the boolean)", () => {
    expect(matched([{ key: "enabled", op: "eq", values: ["true"] }])).toEqual(["t-1", "t-3"]);
    expect(matched([{ key: "enabled", op: "eq", values: ["false"] }])).toEqual(["t-2"]);
  });
  it("offers the interface (resolved name) and enabled value catalogs", () => {
    const byKey = Object.fromEntries(keys.map((k) => [k.key, k]));
    expect(byKey.interface.values!(tasks)).toEqual(["disp-1-icmp", "disp-1-tcp", "sess-1"]);
    expect(byKey.enabled.values!(tasks)).toEqual(["false", "true"]);
  });
});
