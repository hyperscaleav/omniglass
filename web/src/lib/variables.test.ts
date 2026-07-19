import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  listVariables,
  createVariable,
  updateVariable,
  deleteVariable,
  displayValue,
  parseInput,
} from "./variables";

// The data layer is the unit under test; fetch is the seam we fake, so these
// assert the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("variables data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists variables and unwraps the envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ variables: [{ id: "1", name: "poll", value_type: "int", owner_kind: "global", value: 30 }] }),
    );
    const vars = await listVariables();
    expect(vars).toHaveLength(1);
    expect(vars[0].name).toBe("poll");
    expect(vars[0].value).toBe(30);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/variables");
  });

  it("posts the create body with the typed value", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "2", name: "poll", value_type: "int", owner_kind: "location", owner_name: "room", value: 30 }, 201),
    );
    const created = await createVariable({ name: "poll", value_type: "int", owner_kind: "location", owner: "room", value: 30 });
    expect(created.owner_name).toBe("room");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    const sent = await req.json();
    expect(sent).toMatchObject({ name: "poll", owner_kind: "location", owner: "room", value: 30 });
  });

  it("patches the value on update", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "sec_123", name: "poll", value_type: "int", owner_kind: "global", value: 60 }),
    );
    await updateVariable("v1", 60);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain("/api/v1/variables/v1");
    const sent = await req.json();
    expect(sent).toMatchObject({ value: 60 });
  });

  it("throws on an error status", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "unknown value_type" }, 422));
    await expect(createVariable({ name: "x", value_type: "int", owner_kind: "global", value: "no" })).rejects.toBeTruthy();
  });

  it("deletes by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteVariable("v1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/variables/v1");
  });
});

describe("variable value helpers", () => {
  it("renders a value for display", () => {
    expect(displayValue(30)).toBe("30");
    expect(displayValue("HDMI1")).toBe("HDMI1");
    expect(displayValue(true)).toBe("true");
    expect(displayValue({ a: 1 })).toBe('{"a":1}');
    expect(displayValue(null)).toBe("");
  });

  it("parses an input back to the typed value", () => {
    expect(parseInput("int", "30")).toBe(30);
    expect(parseInput("float", "1.5")).toBe(1.5);
    expect(parseInput("bool", "true")).toBe(true);
    expect(parseInput("bool", "false")).toBe(false);
    expect(parseInput("string", "HDMI1")).toBe("HDMI1");
    expect(parseInput("json", '{"a":1}')).toEqual({ a: 1 });
  });

  it("throws on an unparseable number or json", () => {
    expect(() => parseInput("int", "nope")).toThrow();
    expect(() => parseInput("json", "{bad")).toThrow();
  });
});
