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
// The inherited_* facts are the SERVER's answer to "clear this box and you get
// what?" (#716's console half), and they are seeded here with values the browser
// could not possibly have computed: `mic` states a stem of "mic", so a console
// that climbed the chain in TypeScript would print "mic" and every assertion
// below would fail. "from-the-server" can only have been served.
const seed: ComponentType[] = [
  { id: uuidFor("ct-display"), name: "display", label: "Display", official: true, forked: false, stem: "display", abbrev: "fp", icon: "monitor", default_tags: [] },
  { id: uuidFor("ct-interactive-display"), name: "interactive-display", label: "Interactive Display", official: true, forked: false, parent: "display", parent_id: uuidFor("ct-display"), default_tags: [] },
  { id: uuidFor("ct-mic"), name: "mic", label: "Microphone", official: true, forked: false, stem: "mic", abbrev: "mic", icon: "mic", default_tags: [] },
  {
    id: uuidFor("ct-ceiling-mic"), name: "ceiling-mic", label: "Ceiling Microphone", official: false, forked: false,
    parent: "mic", parent_id: uuidFor("ct-mic"), default_tags: [],
    // resolved_icon is what the row SHOWS and inherited_icon is what it would
    // take with its own box cleared: the same string on a row that states no
    // icon, which this one is, and two different questions everywhere else.
    resolved_icon: "icon-from-the-server",
    inherited_stem: "from-the-server", inherited_stem_source: "mic",
    inherited_abbrev: "abbrev-from-the-server", inherited_abbrev_source: "mic",
    inherited_icon: "icon-from-the-server", inherited_icon_source: "mic",
  },
  // A grandchild that states a stem of its own and inherits its abbrev from one
  // level up and its stem source from TWO: the case a hint reading "its parent"
  // gets wrong, and the case a placeholder taken from resolved_* gets wrong.
  {
    id: uuidFor("ct-ceiling-array"), name: "ceiling-array", label: "Ceiling Array", official: false, forked: false,
    parent: "ceiling-mic", parent_id: uuidFor("ct-ceiling-mic"), stem: "carray", default_tags: [],
    inherited_stem: "from-the-server", inherited_stem_source: "mic",
    inherited_abbrev: "abbrev-from-the-server", inherited_abbrev_source: "ceiling-mic",
  },
  // A shipped row the operator has overridden (#655, ADR-0095): the third
  // origin state, and the only one where what the console shows is not what
  // the release ships.
  { id: uuidFor("ct-projector"), name: "projector", label: "House Projector", official: true, forked: true, stem: "projector", abbrev: "proj", icon: "projector", default_tags: [] },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

// factOf scopes one READ-state blade field by its eyebrow label. KVStacked
// renders the eyebrow and the value as siblings under one div, so the eyebrow's
// parent is the whole fact and nothing else. Needed because three fields on this
// blade can inherit from the same ancestor, and a blade-wide text query would
// pass on another field's answer.
function factOf(blade: HTMLElement, label: string): HTMLElement {
  const eyebrow = within(blade).getAllByText(label).find((el) => el.classList.contains("eyebrow"));
  if (!eyebrow?.parentElement) throw new Error(`no read-state fact labelled ${label}`);
  return eyebrow.parentElement;
}

// rowFor and cellOf scope one LIST cell by its row's identity and its column's
// header, so an assertion about the Stem column can never pass on the Abbrev
// column's answer: three columns on this page can inherit from the same
// ancestor, and a row-wide text query would not tell them apart.
function rowFor(label: string): HTMLElement {
  const row = screen
    .getAllByRole("row")
    .slice(1)
    .find((r) => within(r).getAllByRole("cell")[0].textContent?.includes(label));
  if (!row) throw new Error(`no row for ${label}`);
  return row;
}

function cellOf(row: HTMLElement, column: string): HTMLElement {
  const headers = within(screen.getAllByRole("row")[0]).getAllByRole("columnheader");
  const i = headers.findIndex((h) => h.textContent?.trim() === column);
  if (i < 0) throw new Error(`no ${column} column`);
  return within(row).getAllByRole("cell")[i];
}

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

  // #743: the list and the blade have to answer the same question the same way.
  // #742 made the blade show what a row inherits; the Stem and Abbrev cells went
  // on printing an em dash on exactly those rows, so the console said a value
  // was absent while its own detail view, two clicks away, showed the value.
  //
  // The strings asserted are the SERVER's (`inherited_stem` etc.), seeded with
  // values no client-side climb could produce: `mic` states a stem of "mic", so
  // a list that walked the chain in TypeScript would print that and fail here.
  it("shows the value an inheriting row takes, in the list, rather than an em dash", () => {
    mount();
    const row = rowFor("Ceiling Microphone");
    const stem = cellOf(row, "Stem");
    expect(stem.textContent).toContain("from-the-server");
    expect(stem.textContent).not.toContain("\u2014");
    const abbrev = cellOf(row, "Abbrev");
    expect(abbrev.textContent).toContain("abbrev-from-the-server");
    expect(abbrev.textContent).not.toContain("\u2014");
  });

  // A stated value and an inherited one render IDENTICALLY in a table. That is
  // the ruling, not an omission: a table is for scanning values, and the blade
  // is where a value's origin is explained, so the provenance mark is a blade
  // and detail affordance only. One table, one column, both states: Ceiling
  // Microphone takes its stem from elsewhere and Display states its own, and
  // nothing in the row says which is which.
  it("renders an inherited value exactly as a stated one, in the same column", () => {
    mount();
    const inherited = cellOf(rowFor("Ceiling Microphone"), "Stem");
    expect(inherited.textContent).toContain("from-the-server");
    expect(within(inherited).queryByRole("button", { name: /is inherited from/ })).toBeNull();
    const stated = cellOf(rowFor("Display"), "Stem");
    expect(stated.textContent).toContain("display");
    expect(within(stated).queryByRole("button", { name: /is inherited from/ })).toBeNull();
  });

  // Per FACT, not per row: Ceiling Array states a stem of its own and takes its
  // abbrev from one level up, so one row shows a value it chose beside a value
  // it was given, both plainly.
  it("shows a stated fact and an inherited one on the same row, both plainly", () => {
    mount();
    const row = rowFor("Ceiling Array");
    const stem = cellOf(row, "Stem");
    expect(stem.textContent).toContain("carray");
    const abbrev = cellOf(row, "Abbrev");
    expect(abbrev.textContent).toContain("abbrev-from-the-server");
    expect(within(stem).queryByRole("button", { name: /is inherited from/ })).toBeNull();
    expect(within(abbrev).queryByRole("button", { name: /is inherited from/ })).toBeNull();
  });

  // The guard for the ruling, asked of the WHOLE table rather than of the two
  // columns that changed: no provenance mark under any cell, on any row, in any
  // column. The blade's own marks are asserted elsewhere in this file and are
  // untouched, so this is a statement about the surface, not about the feature.
  it("puts no provenance mark anywhere in the table, which is a blade affordance", () => {
    mount();
    const table = screen.getAllByRole("table")[0];
    expect(within(table).queryAllByRole("button", { name: /is inherited from/ })).toHaveLength(0);
  });

  // The em dash keeps its one meaning: nothing here and nothing above. Deleting
  // it outright would be the same defect pointed the other way, a cell claiming
  // a value that does not exist.
  it("still reads an em dash where the row states nothing and nothing above it does", () => {
    mount();
    const stem = cellOf(rowFor("Interactive Display"), "Stem");
    expect(stem.textContent).toContain("\u2014");
    expect(within(stem).queryByRole("button", { name: /is inherited from/ })).toBeNull();
  });

  // The Icon cell has shown the resolved glyph since #695 without saying where
  // it came from, and that is the treatment Stem and Abbrev now MATCH rather
  // than depart from (#743). It is the precedent, not the odd column out.
  it("shows an inherited icon as plainly as a stated one, the precedent the other two match", () => {
    mount();
    const inherited = cellOf(rowFor("Ceiling Microphone"), "Icon");
    expect(inherited.textContent).toContain("icon-from-the-server");
    expect(within(inherited).queryByRole("button", { name: /is inherited from/ })).toBeNull();
    const stated = cellOf(rowFor("Microphone"), "Icon");
    expect(stated.textContent).toContain("mic");
    expect(within(stated).queryByRole("button", { name: /is inherited from/ })).toBeNull();
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

  it("derives the name from the label until the operator edits it", async () => {
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
        return new Response(JSON.stringify({ id: uuidFor("ct-boundary-mic"), name: "boundary-mic", label: "Boundary Microphone", official: false, parent: "mic", parent_id: uuidFor("ct-mic") }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ component_types: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByText("New component type"));
    fireEvent.input(await screen.findByPlaceholderText("Wireless Microphone"), { target: { value: "Boundary Mic" } });
    fireEvent.change(screen.getByLabelText("Parent"), { target: { value: "mic" } });
    fireEvent.click(screen.getByRole("button", { name: /create component type/i }));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).toMatchObject({ name: "boundary-mic", label: "Boundary Mic", parent_id: "mic" });
  });

  // #742 removed the blade hint's present-tense sentence because the provenance
  // mark says it. The CREATE form is a different surface and keeps its own: the
  // row does not exist yet, so the server has served no `inherited_*` answer,
  // no `InheritedField` renders here (these are plain `FieldRow`s) and there is
  // therefore no mark on this form at all. The clause below is the only thing
  // telling an operator that a blank box is a choice rather than an omission, at
  // the one moment they are deciding whether to type a value.
  //
  // It also has to state the one constraint the server enforces at create and
  // the form cannot recover from: `ErrRootComponentTypeNeedsStem` refuses a root
  // with no stem, exactly as `ErrRootSystemTypeNeedsStem` does on the tier
  // beside it. The system form has said so since it shipped and this one never
  // did, so the same operator was warned on one page and met a 422 on the other
  // (#744). The strings are asserted verbatim, on both pages, because the two
  // forms are byte-similar by design and this is the sentence one of them lost.
  it("tells the create form's operator that a blank fact inherits, and that a root's stem is required", async () => {
    mount();
    fireEvent.click(screen.getByText("New component type"));
    await screen.findByPlaceholderText("Wireless Microphone");
    const form = screen.getByPlaceholderText("wireless-mic").closest("form") as HTMLElement;
    expect(within(form).queryByRole("button", { name: /is inherited from/ })).toBeNull();
    expect(within(form).getByText("The auto-generated component name's prefix. Leave blank to inherit the parent's; required on a root.")).toBeTruthy();
    expect(within(form).getByText("Where this type grafts in the tree. Root creates a new top-level genus and then needs a stem of its own; the gateway has no reparent leg, so choose carefully.")).toBeTruthy();
    expect(within(form).getByText("The compact hostname-render form (fp, cam, dsp). Leave blank to inherit.")).toBeTruthy();
    expect(within(form).getByText("A glyph key. Leave blank to inherit.")).toBeTruthy();
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

  // #741: Restore replaces the row UNDERNEATH an open editor, so the drafts have
  // to be re-seeded from what came back. A blade seeds them once, on entering
  // edit, and nothing else moved them, so the fields went on showing the fork
  // the operator had just discarded and the next Save would have written that
  // fork straight back over the shipped values.
  //
  // It asserts the FIELDS rather than that the request went out: the request has
  // always been sent correctly, and sending it is not the behaviour that was
  // broken.
  it("re-seeds the editor from the restored row rather than holding the discarded fork", async () => {
    const shipped: ComponentType = {
      id: uuidFor("ct-projector"), name: "projector", label: "House Projector",
      official: true, forked: false, stem: "projector", abbrev: "proj", icon: "projector", default_tags: [],
    };
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.includes(":restore")) {
        return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ component_types: [shipped] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    vi.spyOn(globalThis, "confirm").mockReturnValue(true);
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    // The operator's fork: every fact on the blade overridden over the shipped
    // row, so a draft that survives the restore is visible in any of them.
    qc.setQueryData([...COMPONENT_TYPES_KEY], [
      { ...shipped, forked: true, label: "Big Projector", stem: "bigproj", abbrev: "bp", icon: "tv" },
    ]);
    qc.setQueryData([...ME_KEY], admin);
    render(() => (
      <QueryClientProvider client={qc}>
        <ComponentTypes />
      </QueryClientProvider>
    ));
    fireEvent.click(screen.getByText("Big Projector"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    const stem = within(blade).getByLabelText("Stem") as HTMLInputElement;
    expect(stem.value).toBe("bigproj");
    // A dirty draft on top of the fork: the operator was mid-edit when they
    // discarded it, which is the state the stale draft is loudest in.
    fireEvent.input(stem, { target: { value: "midedit" } });

    fireEvent.click(within(blade).getByText("Restore default"));
    await waitFor(() => expect((within(blade).getByLabelText("Stem") as HTMLInputElement).value).toBe("projector"));
    expect((within(blade).getByLabelText("Abbrev") as HTMLInputElement).value).toBe("proj");
    expect((within(blade).getByLabelText("Icon") as HTMLInputElement).value).toBe("projector");
    expect((within(blade).getByLabelText("Label") as HTMLInputElement).value).toBe("House Projector");
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
    fireEvent.input(within(blade).getByLabelText("Label") as HTMLInputElement, { target: { value: "Ceiling Mic" } });
    fireEvent.click(within(blade).getByText("Save"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).toMatchObject({ label: "Ceiling Mic", stem: "" });
  });
  // #716's console half: an emptied box has to say what it just fell back to.
  //
  // The values asserted here are the SERVER's (`inherited_stem` etc.), seeded
  // with strings no client-side walk could produce, which is what makes this a
  // test of the read model rather than of a TypeScript climb. #695 deleted that
  // climb and #702 and #710 refused to bring it back; this closes the same gap
  // for the two facts that had no served answer at all.
  it("an inheriting box carries the value it would inherit, and the hint names the ancestor", async () => {
    mount();
    fireEvent.click(screen.getByText("Ceiling Microphone"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    const stem = within(blade).getByLabelText("Stem") as HTMLInputElement;
    expect(stem.value).toBe("");
    expect(stem.placeholder).toBe("from-the-server");
    // The box is empty, so the field IS taking its value from elsewhere right
    // now, and the MARK is what says so. The hint says only what the fact is
    // (#742): the sentence it used to append here restated the mark in the same
    // field at the same instant. The conditional wording ("Empty inherits from")
    // belongs to the state where the box holds a value of its own, and the two
    // never overlap.
    expect(within(blade).getByText("The auto-generated component name's prefix.")).toBeTruthy();
    expect(within(blade).queryByText(/[Ii]nherit(ed|s) from/)).toBeNull();
    expect(within(blade).getByRole("button", { name: "Stem is inherited from mic" })).toBeTruthy();
    // The other two carry their own served answers rather than the stem's.
    expect((within(blade).getByLabelText("Abbrev") as HTMLInputElement).placeholder).toBe("abbrev-from-the-server");
    expect((within(blade).getByLabelText("Icon") as HTMLInputElement).placeholder).toBe("icon-from-the-server");
  });

  // The ancestor is read from the data, not described as "its parent": Ceiling
  // Array's parent is Ceiling Microphone, and its stem comes from mic, two
  // levels up. A hint that named the parent would send the operator to the
  // wrong row to change it for everything below.
  it("names the ancestor a fact actually came from, which may not be the parent", async () => {
    mount();
    fireEvent.click(screen.getByText("Ceiling Array"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    // The stem states its own value here, so it gets the conditional sentence
    // and no mark; the abbrev is inheriting, so it gets the mark and its hint
    // says only what the fact is. One blade, both states, and the two ancestors
    // are named from the data at two different distances up the chain: the stem
    // by the sentence (`mic`, two levels up) and the abbrev by the mark
    // (`ceiling-mic`, one).
    expect(within(blade).getByText(/The auto-generated component name's prefix\. Empty inherits from mic\./)).toBeTruthy();
    expect(within(blade).getByText("The compact hostname-render form (fp, cam, dsp).")).toBeTruthy();
    expect(within(blade).getByRole("button", { name: "Abbrev is inherited from ceiling-mic" })).toBeTruthy();
    expect(within(blade).queryByRole("button", { name: /^Stem is inherited from/ })).toBeNull();
  });

  // The placeholder answers "clear this box and you get what?", so it must be
  // the ANCESTOR's value even while the row still states its own. A console
  // that used the row's shown value would print the string the operator had
  // just deleted back at them as the thing they were about to inherit.
  it("a box that states its own value still offers the inherited one behind it", async () => {
    mount();
    fireEvent.click(screen.getByText("Ceiling Array"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    const stem = within(blade).getByLabelText("Stem") as HTMLInputElement;
    expect(stem.value).toBe("carray");
    expect(stem.placeholder).toBe("from-the-server");
    fireEvent.input(stem, { target: { value: "" } });
    expect(stem.value).toBe("");
    expect(stem.placeholder).toBe("from-the-server");
  });

  // Read mode is where the operator lands the moment Save leaves edit, so it
  // has to answer the same question: before this it rendered an em dash and
  // told them nothing about what they had just fallen back to.
  //
  // The ancestor is named by the MARK rather than by a sentence under the value
  // (the sentence shipped in this wave and came out): the value line carries the
  // value, the dot beside the label carries the relation, and it is the same dot
  // in the same place the edit state shows.
  it("reads an inheriting fact as the inherited value, marked with where it came from", async () => {
    mount();
    fireEvent.click(screen.getByText("Ceiling Microphone"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    const stemFact = factOf(blade, "Stem");
    expect(stemFact.textContent).toContain("from-the-server");
    expect(within(stemFact).getByRole("button", { name: "Stem is inherited from mic" })).toBeTruthy();
    expect(stemFact.textContent).not.toContain("inherited from");
    // Not the em dash it used to be, which is the whole defect.
    expect(stemFact.textContent).not.toContain("\u2014");
  });

  // A node with a fact of its own shows that fact normally: no attribution, and
  // above all no LOCK. ADR-0104 gives the lock one meaning, the platform owns
  // this value, and an inherited fact is one an operator MAY set, which is the
  // opposite. The pen's two buttons are named here so borrowing either one
  // fails.
  it("shows a fact the row states normally, with no inheritance marker and no lock", async () => {
    mount();
    fireEvent.click(screen.getByText("Ceiling Array"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    const stemFact = factOf(blade, "Stem");
    expect(stemFact.textContent).toContain("carray");
    expect(within(stemFact).queryByRole("button", { name: /is inherited from/ })).toBeNull();
    // Its abbrev IS inheriting, on the same blade, so the absence above is a
    // per-field answer rather than a blade with the feature switched off.
    expect(
      within(factOf(blade, "Abbrev")).getByRole("button", { name: "Abbrev is inherited from ceiling-mic" }),
    ).toBeTruthy();
    expect(within(blade).queryByLabelText(/^Override the /)).toBeNull();
    expect(within(blade).queryByLabelText(/^Restore the .* to default$/)).toBeNull();
  });

  // A root has nothing above it, so there is no inherited value to offer and no
  // ancestor to name. The field must not invent either.
  it("offers no inherited value on a root, which has nothing above it", async () => {
    mount();
    fireEvent.click(screen.getByText("Display"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    const stem = within(blade).getByLabelText("Stem") as HTMLInputElement;
    fireEvent.input(stem, { target: { value: "" } });
    expect(stem.placeholder).toBe("");
    expect(within(blade).queryByText(/inherits from/)).toBeNull();
    expect(within(blade).getByText(/The auto-generated component name's prefix\. Nothing above this states one\./)).toBeTruthy();
    // An empty box on a root is genuinely empty, not inheriting, so there is
    // nothing for the mark to point at and it must not invent one.
    expect(within(blade).queryByRole("button", { name: /is inherited from/ })).toBeNull();
  });
});
