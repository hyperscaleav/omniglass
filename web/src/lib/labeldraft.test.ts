import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  nameRefused,
  ordinalTaken,
  recoverFromTakenOrdinal,
  renderComponentLabel,
  renderLocationLabel,
  renderSystemLabel,
} from "./labeldraft";

// The draft data layer (#699, #702). fetch is the seam, so these assert the
// request SHAPE, which is the half that has to match the create body posted a
// moment later: the same field names, and a name that is omitted rather than
// sent as "" when the platform holds the pen.
//
// The second half is the two refusals a form ACTS on, told apart by the field
// each is located on rather than by matching server copy.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("the draft label data layer", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("asks the component route with the classification and placement the form holds", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ label: "Microphone n", rule: "{{.TypeName}} {{.Ordinal}}" }),
    );
    const got = await renderComponentLabel({ product: "shure-mxa920", location: "room-204b" });
    expect(got).toEqual({ label: "Microphone n", rule: "{{.TypeName}} {{.Ordinal}}" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/components:renderLabel");
    const body = JSON.parse(await req.clone().text());
    // The name is ABSENT, not empty: omitted is "the platform names this",
    // exactly as it is on the create, so the two requests cannot describe two
    // different rows.
    expect("name" in body).toBe(false);
    expect(body).toMatchObject({ product: "shure-mxa920", location: "room-204b" });
  });

  it("carries an operator's name through when they hold the pen", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ label: "Microphone", rule: "{{.TypeName}}" }));
    await renderComponentLabel({ product: "shure-mxa920", name: "front-mic" });
    const body = JSON.parse(await (fetchMock.mock.calls[0][0] as Request).clone().text());
    expect(body.name).toBe("front-mic");
  });

  it("names the system's classifiers with the same keys the create body uses", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ label: "Boardroom", rule: "{{.TypeName}}" }));
    await renderSystemLabel({ system_type_id: "board", standard_id: "huddle", location: "room-204b" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/systems:renderLabel");
    expect(JSON.parse(await req.clone().text())).toMatchObject({
      system_type_id: "board",
      standard_id: "huddle",
      location: "room-204b",
    });
  });

  it("reads an empty label as an answer rather than a failure", async () => {
    // No rule resolves at any tier, so nothing is stored and the surface reads
    // the name. A shipped estate no longer lands here (a location rule ships as
    // of #657), which is why the empty answer needs a test of its own rather
    // than being the case every form happened to hit.
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ label: "", rule: "" }));
    expect(await renderLocationLabel({ location_type: "room", name: "boardroom" })).toEqual({ label: "", rule: "" });
  });

  it("throws the API's refusal rather than swallowing it", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ title: "Unprocessable Entity", detail: "this location_type has no name rule" }, 422),
    );
    await expect(renderLocationLabel({ location_type: "room" })).rejects.toBeTruthy();
  });

  it("returns the name and the ordinal beside the label", async () => {
    // The name is the server's since #702, so the form shows a real number and
    // has something to post back as its precondition.
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ name: "mic-3", ordinal: 3, label: "Microphone 3", rule: "{{.TypeName}} {{.Ordinal}}" }),
    );
    expect(await renderComponentLabel({ product: "shure-mxa920", location: "room-204b" })).toEqual({
      name: "mic-3",
      ordinal: 3,
      label: "Microphone 3",
      rule: "{{.TypeName}} {{.Ordinal}}",
    });
  });

  it("sends the parent, which is half the bucket the ordinal is read from", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ name: "mic-1", ordinal: 1, label: "", rule: "" }));
    await renderComponentLabel({ product: "shure-mxa920", parent: "rack-a" });
    expect(JSON.parse(await (fetchMock.mock.calls[0][0] as Request).clone().text())).toMatchObject({ parent: "rack-a" });
  });
});

// The two refusals, read out of the RFC 9457 errors array. A form that matched
// on the sentence would break the first time the server rewrote it, and a form
// that matched on the STATUS would confuse a moved ordinal with a name
// collision, whose recovery is the operator's rather than the form's.
describe("the refusals a create form acts on", () => {
  const refusal = (location: string, value?: unknown) => ({
    status: 409,
    detail: "something happened",
    errors: [{ message: "m", location, value }],
  });

  it("recognises the platform declining to name the row", () => {
    expect(nameRefused(refusal("body.name"))).toBe(true);
    expect(nameRefused(refusal("body.product"))).toBe(false);
    expect(nameRefused({ status: 422, detail: "unknown location_type" })).toBe(false);
    expect(nameRefused(null)).toBe(false);
  });

  it("reads the ordinal that moved out of the conflict", () => {
    expect(ordinalTaken(refusal("body.expected_ordinal", 4))).toBe(4);
  });

  it("is not fooled by the other 409 a create can give", () => {
    // A name collision: same status, different recovery, and re-reading the
    // draft would tell that operator nothing.
    expect(ordinalTaken({ status: 409, detail: "component name already exists" })).toBeNull();
    expect(ordinalTaken(refusal("body.name"))).toBeNull();
  });

  it("re-reads the draft only for the ordinal conflict, and says it did", async () => {
    const refetch = vi.fn().mockResolvedValue(undefined);
    const msg = await recoverFromTakenOrdinal(refusal("body.expected_ordinal", 4), refetch);
    expect(refetch).toHaveBeenCalledTimes(1);
    expect(msg).toContain("something happened");
    expect(msg).toMatch(/create again/i);
  });

  it("leaves every other refusal exactly as it reads, and re-reads nothing", async () => {
    const refetch = vi.fn().mockResolvedValue(undefined);
    const msg = await recoverFromTakenOrdinal({ detail: "component name already exists" }, refetch);
    expect(refetch).not.toHaveBeenCalled();
    expect(msg).toBe("component name already exists");
  });
});
