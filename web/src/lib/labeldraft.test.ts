import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderComponentLabel, renderLocationLabel, renderSystemLabel } from "./labeldraft";

// The draft-label data layer (#699). fetch is the seam, so these assert the
// request SHAPE, which is the half that has to match the create body posted a
// moment later: the same field names, and a name that is omitted rather than
// sent as "" when the platform holds the pen.
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
});
