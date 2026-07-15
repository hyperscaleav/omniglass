import { describe, it, expect, vi, beforeEach } from "vitest";
import { listTasks, getTask } from "./tasks";

// The data layer is the unit under test; fetch is the seam we fake, so these assert
// the request shape and the response handling without a server. A task is read-only,
// so only the read wrappers exist.
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
