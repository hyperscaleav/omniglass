import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ComponentTypes from "./ComponentTypes";
import { COMPONENT_TYPES_KEY, type ComponentType } from "../lib/component_types";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// Component Types (Catalog > Components: Types): the device-class genus
// registry (ADR-0085, partially reversing ADR-0047), on the same
// official/custom FlatList pattern as Location Types, but tree-shaped
// (parent_id) rather than flat: a subtype (interactive-display) grafts under
// a root (display), and every identity fact (stem, icon, abbrev) inherits
// down the tree unless a node overrides it. There is no reparent leg (the
// gateway does not offer one yet), so a custom type's placement in the tree
// is fixed at create; the edit blade may only revise a node's own facts.
const seed: ComponentType[] = [
  { id: uuidFor("ct-display"), name: "display", display_name: "Display", official: true, forked: false, stem: "display", abbrev: "fp", icon: "monitor", default_tags: [] },
  { id: uuidFor("ct-interactive-display"), name: "interactive-display", display_name: "Interactive Display", official: true, forked: false, parent: "display", parent_id: uuidFor("ct-display"), default_tags: [] },
  { id: uuidFor("ct-mic"), name: "mic", display_name: "Microphone", official: true, forked: false, stem: "mic", abbrev: "mic", icon: "mic", default_tags: [] },
  { id: uuidFor("ct-ceiling-mic"), name: "ceiling-mic", display_name: "Ceiling Microphone", official: false, forked: false, parent: "mic", parent_id: uuidFor("ct-mic"), default_tags: [] },
  // A shipped row the operator has overridden (#655, ADR-0095): the third
  // origin state, and the only one where what the console shows is not what
  // the release ships.
  { id: uuidFor("ct-projector"), name: "projector", display_name: "House Projector", official: true, forked: true, stem: "projector", abbrev: "proj", icon: "projector", default_tags: [] },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...COMPONENT_TYPES_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <ComponentTypes />
    </QueryClientProvider>
  ));
}

describe("ComponentTypes page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders the seeded tree, a child row immediately after its parent", () => {
    mount();
    const rows = screen.getAllByRole("row").slice(1); // drop the header row
    const names = rows.map((r) => within(r).getAllByRole("cell")[0].textContent);
    const displayAt = names.findIndex((t) => t?.includes("Display"));
    const interactiveAt = names.findIndex((t) => t?.includes("Interactive Display"));
    const micAt = names.findIndex((t) => t?.includes("Microphone"));
    const ceilingAt = names.findIndex((t) => t?.includes("Ceiling Microphone"));
    expect(displayAt).toBeGreaterThanOrEqual(0);
    expect(interactiveAt).toBe(displayAt + 1);
    expect(ceilingAt).toBe(micAt + 1);
  });

  it("shows New component type for a caller holding component_type:create", () => {
    mount(admin);
    expect(screen.getByText("New component type")).toBeTruthy();
  });

  it("hides New component type for a caller without component_type:create", () => {
    mount(viewer);
    expect(screen.queryByText("New component type")).toBeNull();
  });

  it("offers the seeded tree, with Root, in the create form's Parent picker", async () => {
    mount();
    fireEvent.click(screen.getByText("New component type"));
    const parentSelect = (await screen.findByLabelText("Parent")) as HTMLSelectElement;
    const labels = Array.from(parentSelect.options).map((o) => o.textContent?.trim());
    expect(labels[0]).toMatch(/root/i);
    expect(labels).toContain("Display");
    expect(labels).toContain("Microphone");
  });

  it("derives the name from the display name until the operator edits it", async () => {
    mount();
    fireEvent.click(screen.getByText("New component type"));
    const display = (await screen.findByPlaceholderText("Wireless Microphone")) as HTMLInputElement;
    const name = screen.getByPlaceholderText("wireless-mic") as HTMLInputElement;
    fireEvent.input(display, { target: { value: "Boundary Microphone" } });
    expect(name.value).toBe("boundary-microphone");
  });

  it("sends the chosen parent and facts on create", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.endsWith("/component-types")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ id: uuidFor("ct-boundary-mic"), name: "boundary-mic", display_name: "Boundary Microphone", official: false, parent: "mic", parent_id: uuidFor("ct-mic") }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ component_types: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByText("New component type"));
    fireEvent.input(await screen.findByPlaceholderText("Wireless Microphone"), { target: { value: "Boundary Mic" } });
    fireEvent.change(screen.getByLabelText("Parent"), { target: { value: "mic" } });
    fireEvent.click(screen.getByRole("button", { name: /create component type/i }));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).toMatchObject({ name: "boundary-mic", display_name: "Boundary Mic", parent_id: "mic" });
  });

  it("a shipped row offers Edit (the edit forks it) to a caller holding update, and none to a viewer", async () => {
    mount(admin);
    fireEvent.click(screen.getByText("Display"));
    const officialBlade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect((within(officialBlade).getByLabelText("Edit") as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(screen.getByText("Ceiling Microphone"));
    const customBlade = await waitFor(() => {
      const els = asides();
      const el = els[els.length - 1];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect((within(customBlade).getByLabelText("Edit") as HTMLButtonElement).disabled).toBe(false);
  });

  it("a viewer cannot edit a shipped row, so cannot fork one", async () => {
    mount(viewer);
    fireEvent.click(screen.getByText("Display"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect((within(blade).getByLabelText("Edit") as HTMLButtonElement).disabled).toBe(true);
  });

  it("names the three origins: official, custom, and overridden", () => {
    mount();
    const rows = screen.getAllByRole("row").slice(1);
    const originOf = (label: string) => {
      const row = rows.find((r) => within(r).getAllByRole("cell")[0].textContent?.includes(label));
      if (!row) throw new Error(`no row for ${label}`);
      const cells = within(row).getAllByRole("cell").map((c) => c.textContent?.trim());
      return cells.find((t) => t === "official" || t === "custom" || t === "overridden");
    };
    expect(originOf("Interactive Display")).toBe("official");
    expect(originOf("Ceiling Microphone")).toBe("custom");
    expect(originOf("House Projector")).toBe("overridden");
  });

  it("a forked shipped row offers Restore default, and a pristine one offers nothing to discard", async () => {
    let restored: string | undefined;
    vi.spyOn(globalThis, "confirm").mockReturnValue(true);
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.includes(":restore")) {
        restored = req.url;
        return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ component_types: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();

    fireEvent.click(screen.getByText("Display"));
    const pristine = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(pristine).queryByText("Restore default")).toBeNull();
    // A shipped row that is still pristine greys its destructive slot with the
    // official sentence: nothing to delete, nothing yet to discard.
    expect((within(pristine).getByLabelText("Delete") as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getByText("House Projector"));
    const forked = await waitFor(() => {
      const els = asides();
      const el = els[els.length - 1];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(forked).queryByLabelText("Delete")).toBeNull();
    fireEvent.click(within(forked).getByText("Restore default"));
    await waitFor(() => expect(restored).toBeTruthy());
    expect(restored).toContain(`${uuidFor("ct-projector")}:restore`);
  });

  it("edit mode exposes stem/abbrev/icon fields and saves them, never the parent", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH" && req.url.includes("/component-types/")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ component_types: seed }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByText("Ceiling Microphone"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    const stemInput = within(blade).getByLabelText("Stem") as HTMLInputElement;
    fireEvent.input(stemInput, { target: { value: "ceiling-mic" } });
    fireEvent.click(within(blade).getByText("Save"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).toMatchObject({ stem: "ceiling-mic" });
    expect(sent).not.toHaveProperty("parent_id");
    // An empty box rides as the three-state string sentinel "" (#716), which
    // is the wire's spelling of "this node declares no fact of its own": the
    // patch routes all three through a CASE where "" clears to NULL, so the
    // inheritance walk resumes at the nearest ancestor. This INVERTS what #677
    // asserted here (an inherited fact rode as OMITTED, because the coalescing
    // patch would otherwise have written a real empty value and stopped the
    // walk). Ceiling Microphone carries no icon and no abbrev of its own, so
    // those two are the empty-box case.
    expect(sent).toMatchObject({ icon: "", abbrev: "" });
  });

  // The clearing move #716 exists for, and the one the console could not spell
  // at all: a node that HAS its own fact is edited back to inheriting its
  // parent's. Display carries a stem, an abbrev and an icon; emptying every box
  // has to reach the server as the sentinel on each, not as a silent no-op that
  // leaves the value the operator just deleted in place.
  it("sends the sentinel for a fact the operator cleared back to inheriting", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH" && req.url.includes("/component-types/")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ component_types: seed }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByText("Display"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    for (const label of ["Stem", "Abbrev", "Icon"]) {
      fireEvent.input(within(blade).getByLabelText(label) as HTMLInputElement, { target: { value: "" } });
    }
    fireEvent.click(within(blade).getByText("Save"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).toMatchObject({ stem: "", abbrev: "", icon: "" });
  });

  // The second leg of #677, converted by #716: a custom child with no stem of
  // its own is legal (the server requires a stem only on a root), and the empty
  // box now rides as the sentinel rather than being dropped. It is a no-op
  // against a column already NULL, and it is the same body a clear sends, which
  // is the point: the console has one spelling for an empty box.
  it("edits a custom child that has no stem of its own, sending the sentinel", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH" && req.url.includes("/component-types/")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ component_types: seed }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByText("Ceiling Microphone"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    fireEvent.input(within(blade).getByLabelText("Display name") as HTMLInputElement, { target: { value: "Ceiling Mic" } });
    fireEvent.click(within(blade).getByText("Save"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).toMatchObject({ display_name: "Ceiling Mic", stem: "" });
  });
});
