import { describe, it, expect } from "vitest";
import { createSignal } from "solid-js";
import { render, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import LocationSelect from "./LocationSelect";
import { LOCATIONS_KEY, type Location } from "../lib/locations";
import { LOCATION_TYPES_KEY, type LocationType } from "../lib/location_types";
import { uuidFor } from "../lib/testids";
import { openTree, pickTree, treeOptions } from "./TreeSelect.testkit";

const types = [
  { name: "campus", allowed_parent_types: [] },
  { name: "building", allowed_parent_types: ["campus"] },
  { name: "room", allowed_parent_types: ["building"] },
] as unknown as LocationType[];
const locs = [
  { id: uuidFor("r1"), name: "board", label: "Boardroom", location_type: "room", parent_id: uuidFor("b1"), path: "hq.b1.board" },
  { id: uuidFor("b1"), name: "b1", label: "Building 1", location_type: "building", parent_id: uuidFor("c1"), path: "hq.b1" },
  { id: uuidFor("c1"), name: "hq", label: "HQ", location_type: "campus", path: "hq" },
] as Location[];

function mount(ui: () => import("solid-js").JSX.Element, seed = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  if (seed) {
    qc.setQueryData(LOCATIONS_KEY, locs);
    qc.setQueryData(LOCATION_TYPES_KEY, types);
  }
  render(() => <QueryClientProvider client={qc}>{ui()}</QueryClientProvider>);
  return qc;
}
const trigger = () => screen.getByRole("button") as HTMLElement;

describe("LocationSelect", () => {
  it("lists the fleet's locations as a tree, root option first", async () => {
    mount(() => <LocationSelect value="" onChange={() => {}} rootLabel="None" />);
    expect(await treeOptions(trigger())).toEqual(["None", "HQ", "Building 1", "Boardroom"]);
  });

  it("commits the uuid by default and the address a caller asks for", async () => {
    const picked: string[] = [];
    mount(() => (
      <>
        <LocationSelect value="" onChange={(v) => picked.push(v)} />
        <LocationSelect value="" onChange={(v) => picked.push(v)} addressOf={(l) => l.path || l.name} />
      </>
    ));
    const [a, b] = screen.getAllByRole("button") as HTMLElement[];
    await pickTree(a, "Boardroom");
    await pickTree(b, "Boardroom");
    expect(picked).toEqual([uuidFor("r1"), "hq.b1.board"]);
  });

  it("shows a selection made before the locations land, with its ancestor path", async () => {
    const qc = mount(() => <LocationSelect value="hq" onChange={() => {}} addressOf={(l) => l.name} rootLabel="(unplaced)" />, false);
    expect(trigger().textContent).toBe("(unplaced)");
    qc.setQueryData(LOCATION_TYPES_KEY, types);
    qc.setQueryData(LOCATIONS_KEY, locs);
    await waitFor(() => expect(trigger().textContent).toBe("HQ"));
  });

  it("keeps a location that may not be picked in the tree but refuses it", async () => {
    const [v, setV] = createSignal("");
    mount(() => <LocationSelect value={v()} onChange={setV} pickable={(l) => l.location_type === "building"} />);
    const options = await openTree(trigger());
    expect(options.map((o) => [o.textContent, o.getAttribute("aria-disabled") === "true"])).toEqual([
      ["HQ", true],
      ["Building 1", false],
      ["Boardroom", true],
    ]);
  });

  it("leaves a location and its subtree out of its own reparent choices", async () => {
    mount(() => <LocationSelect value="" onChange={() => {}} excludeSubtreeOf={uuidFor("b1")} rootLabel="Root" />);
    expect(await treeOptions(trigger())).toEqual(["Root", "HQ"]);
  });
});
