import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import SystemTypes from "./SystemTypes";
import { SYSTEM_TYPES_KEY, type SystemType } from "../lib/system_types";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// System Types (Catalog > Systems: Types): the coarse space taxonomy
// (ADR-0096), on the same official/custom FlatList pattern as Component Types
// and tree-shaped by parent_id. The registry is NOT the standard: the type says
// what kind of space this is, the standard says which blueprint it is built to.
// Every identity fact (stem, abbrev, icon) inherits down the tree unless a node
// overrides it, and there is no reparent leg, so a custom type's placement is
// fixed at create.
const seed: SystemType[] = [
  { id: uuidFor("st-av"), name: "av", display_name: "AV", official: true, stem: "av", abbrev: "av", icon: "layers", resolved_icon: "layers" },
  { id: uuidFor("st-room"), name: "room", display_name: "Room", official: true, parent: "av", parent_id: uuidFor("st-av"), stem: "room", abbrev: "rm", icon: "door-open", resolved_icon: "door-open" },
  { id: uuidFor("st-board"), name: "board", display_name: "Boardroom", official: true, parent: "room", parent_id: uuidFor("st-room"), stem: "boardroom", abbrev: "br", resolved_icon: "door-open" },
  {
    id: uuidFor("st-lab"), name: "lab", display_name: "Lab", official: false,
    parent: "room", parent_id: uuidFor("st-room"), stem: "lab", abbrev: "lab", resolved_icon: "door-open",
    // The SERVER's answer to "clear this box and you get what?" (#716), seeded
    // with strings no client-side climb could produce: `av`, two levels up,
    // states an icon of "layers", so a console that walked the chain in
    // TypeScript would print that and fail here.
    inherited_icon: "icon-from-the-server", inherited_icon_source: "av",
    inherited_stem: "stem-from-the-server", inherited_stem_source: "room",
  },
  // A custom row that states NONE of the three, so the list has an inheriting
  // row to put beside a stating one in the same columns (#743). Its three facts
  // come from two different distances up the chain, and resolved_icon is what
  // the server says it SHOWS, which on a row stating no icon is the value it
  // takes.
  {
    id: uuidFor("st-studio"), name: "studio", display_name: "Studio", official: false,
    parent: "room", parent_id: uuidFor("st-room"), resolved_icon: "icon-from-the-server",
    inherited_stem: "stem-from-the-server", inherited_stem_source: "room",
    inherited_abbrev: "abbrev-from-the-server", inherited_abbrev_source: "av",
    inherited_icon: "icon-from-the-server", inherited_icon_source: "av",
  },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

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
  qc.setQueryData([...SYSTEM_TYPES_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <SystemTypes />
    </QueryClientProvider>
  ));
}

describe("SystemTypes page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders the shipped tree, a child row immediately after its parent", () => {
    mount();
    const rows = screen.getAllByRole("row").slice(1); // drop the header row
    const names = rows.map((r) => within(r).getAllByRole("cell")[0].textContent);
    const avAt = names.findIndex((t) => t?.includes("AV"));
    const roomAt = names.findIndex((t) => t?.includes("Room"));
    const boardAt = names.findIndex((t) => t?.includes("Boardroom"));
    expect(avAt).toBe(0);
    expect(roomAt).toBe(avAt + 1);
    // Alphabetical within a level: Boardroom before Lab.
    expect(boardAt).toBe(roomAt + 1);
  });

  it("shows the resolved icon the server sent, not the row's own blank one", () => {
    mount();
    const rows = screen.getAllByRole("row").slice(1);
    const boardRow = rows.find((r) => within(r).getAllByRole("cell")[0].textContent?.includes("Boardroom"))!;
    // board sets no icon of its own, so the Icon cell names the one the server
    // resolved for it (room's, not the root av's).
    expect(boardRow.textContent).toContain("door-open");
    expect(boardRow.textContent).not.toContain("layers");
  });

  // #743, the system registry's half: the same defect, the same fix, the same
  // vocabulary. The strings are the SERVER's, seeded so that a list climbing the
  // chain in TypeScript would print `room`'s real stem and fail here.
  it("shows the value an inheriting row takes, in the list, rather than an em dash", () => {
    mount();
    const row = rowFor("Studio");
    const stem = cellOf(row, "Stem");
    expect(stem.textContent).toContain("stem-from-the-server");
    expect(stem.textContent).not.toContain("\u2014");
    expect(within(stem).getByRole("button", { name: "Stem is inherited from room" })).toBeTruthy();
    const abbrev = cellOf(row, "Abbrev");
    expect(abbrev.textContent).toContain("abbrev-from-the-server");
    expect(within(abbrev).getByRole("button", { name: "Abbrev is inherited from av" })).toBeTruthy();
  });

  // Both values in these columns are muted already, so the DOT is the whole
  // distinction. One table, one column, both states.
  it("marks an inherited value and leaves a stated one unmarked, in the same column", () => {
    mount();
    const stated = cellOf(rowFor("Lab"), "Stem");
    expect(stated.textContent).toContain("lab");
    expect(within(stated).queryByRole("button", { name: /is inherited from/ })).toBeNull();
    const inherited = cellOf(rowFor("Studio"), "Stem");
    expect(within(inherited).getByRole("button", { name: "Stem is inherited from room" })).toBeTruthy();
  });

  // The Icon cell has shown the resolved glyph since #695 and never said where
  // it came from. Boardroom is the control: it states no icon and the server
  // served it no inherited answer, so the cell names what it shows and marks
  // nothing.
  it("marks an inherited icon too, and marks nothing where no ancestor was named", () => {
    mount();
    const marked = cellOf(rowFor("Studio"), "Icon");
    expect(marked.textContent).toContain("icon-from-the-server");
    expect(within(marked).getByRole("button", { name: "Icon is inherited from av" })).toBeTruthy();
    const unmarked = cellOf(rowFor("Boardroom"), "Icon");
    expect(unmarked.textContent).toContain("door-open");
    expect(within(unmarked).queryByRole("button", { name: /is inherited from/ })).toBeNull();
  });

  it("shows New system type only for a caller holding system_type:create", () => {
    mount(admin);
    expect(screen.getByText("New system type")).toBeTruthy();
    mount(viewer);
    expect(screen.queryAllByText("New system type")).toHaveLength(1); // the admin mount's, not a second
  });

  it("offers the shipped tree, with Root, in the create form's Parent picker", async () => {
    mount();
    fireEvent.click(screen.getByText("New system type"));
    const parentSelect = (await screen.findByLabelText("Parent")) as HTMLSelectElement;
    const labels = Array.from(parentSelect.options).map((o) => o.textContent?.trim());
    expect(labels[0]).toMatch(/root/i);
    expect(labels).toContain("AV");
    expect(labels).toContain("Boardroom");
  });

  it("sends the chosen parent and facts on create", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.endsWith("/system-types")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ id: uuidFor("st-lecture"), name: "lecture", display_name: "Lecture Hall", official: false, parent: "room", parent_id: uuidFor("st-room") }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ system_types: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByText("New system type"));
    fireEvent.input(await screen.findByPlaceholderText("Huddle Room"), { target: { value: "Lecture Hall" } });
    fireEvent.change(screen.getByLabelText("Parent"), { target: { value: "room" } });
    fireEvent.click(screen.getByRole("button", { name: /create system type/i }));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).toMatchObject({ name: "lecture-hall", display_name: "Lecture Hall", parent_id: "room" });
  });

  // The create form's twin of the component registry's, and it carries one
  // thing more: a ROOT type must state a stem, because there is no ancestor to
  // take one from (`ErrRootSystemTypeNeedsStem` at the gateway). #742 removed
  // the blade hint's present-tense sentence, which the mark replaced; neither
  // the mark nor `InheritedField` reaches this form, and nothing at all conveys
  // the root constraint, so both halves of this hint stay.
  it("tells the create form's operator that a blank fact inherits, and that a root's stem is required", async () => {
    mount();
    fireEvent.click(screen.getByText("New system type"));
    await screen.findByPlaceholderText("Huddle Room");
    const form = screen.getByPlaceholderText("huddle").closest("form") as HTMLElement;
    expect(within(form).queryByRole("button", { name: /is inherited from/ })).toBeNull();
    expect(within(form).getByText("The prefix a generated system name is built from. Leave blank to inherit the parent's; required on a root.")).toBeTruthy();
    // Pinned verbatim on both pages (#744): the two create forms are
    // byte-similar by design, and the root-stem rule is one rule the server
    // enforces on both tiers, so a change to either wording has to fail here.
    expect(within(form).getByText("Where this type grafts in the tree. Root creates a new top-level genus and then needs a stem of its own; the gateway has no reparent leg, so choose carefully.")).toBeTruthy();
    expect(within(form).getByText("The compact label form (br, cls, vw). Leave blank to inherit.")).toBeTruthy();
    expect(within(form).getByText("A glyph key. Leave blank to inherit.")).toBeTruthy();
  });

  it("an official row greys Edit; a custom row carries it live", async () => {
    mount();
    fireEvent.click(screen.getByText("Boardroom"));
    const officialBlade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect((within(officialBlade).getByLabelText("Edit") as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getByText("Lab"));
    const customBlade = await waitFor(() => {
      const els = asides();
      const el = els[els.length - 1];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect((within(customBlade).getByLabelText("Edit") as HTMLButtonElement).disabled).toBe(false);
  });

  it("edit mode exposes stem/abbrev/icon and saves them, never the parent", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH" && req.url.includes("/system-types/")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ system_types: seed }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByText("Lab"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    fireEvent.input(within(blade).getByLabelText("Abbrev") as HTMLInputElement, { target: { value: "lb" } });
    fireEvent.click(within(blade).getByText("Save"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).toMatchObject({ abbrev: "lb" });
    expect(sent).not.toHaveProperty("parent_id");
    // An empty box rides as the three-state string sentinel "" (#716), the
    // wire's spelling of "this node declares no fact of its own": the patch
    // routes all three through a CASE where "" clears to NULL and the
    // inheritance walk resumes at the nearest ancestor. This INVERTS what #656
    // asserted here (an inherited fact rode as OMITTED, because the coalescing
    // patch would otherwise have written a real empty value). Lab carries no
    // icon of its own, so that one is the empty-box case.
    expect(sent).toMatchObject({ icon: "" });
  });

  // #716's clearing move on this registry: a node that HAS its own fact edited
  // back to inheriting its parent's. Lab carries a stem and an abbrev, so
  // emptying both boxes has to reach the server as the sentinel on each.
  it("sends the sentinel for a fact the operator cleared back to inheriting", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH" && req.url.includes("/system-types/")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ system_types: seed }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByText("Lab"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    for (const label of ["Stem", "Abbrev"]) {
      fireEvent.input(within(blade).getByLabelText(label) as HTMLInputElement, { target: { value: "" } });
    }
    fireEvent.click(within(blade).getByText("Save"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).toMatchObject({ stem: "", abbrev: "" });
  });

  // #716's console half on the system registry: the same field, the same
  // vocabulary, the same served answers. Lab states a stem and an abbrev and no
  // icon, so one field is inheriting and two are not, on one blade.
  it("shows what an inherited fact inherits, and names the ancestor it came from", async () => {
    mount();
    fireEvent.click(screen.getByText("Lab"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // Read mode first: the icon is inheriting, so it reads as the value it
    // takes rather than as an em dash, attributed two levels up.
    const iconEyebrow = within(blade).getAllByText("Icon").find((el) => el.classList.contains("eyebrow"));
    const iconFact = iconEyebrow!.parentElement!;
    expect(iconFact.textContent).toContain("icon-from-the-server");
    // The ancestor is named by the mark beside the label, not by a sentence
    // under the value, and the mark is the same one the edit state shows.
    expect(within(iconFact).getByRole("button", { name: "Icon is inherited from av" })).toBeTruthy();
    expect(iconFact.textContent).not.toContain("inherited from");

    fireEvent.click(within(blade).getByLabelText("Edit"));
    const icon = within(blade).getByLabelText("Icon") as HTMLInputElement;
    expect(icon.value).toBe("");
    expect(icon.placeholder).toBe("icon-from-the-server");
    // The mark says where the value came from; the hint says only what the fact
    // is (#742). The relation is stated once, by the affordance that survives
    // both of the field's states.
    expect(within(blade).getByText("A glyph key.")).toBeTruthy();
    expect(within(blade).queryByText(/Inherited from av/)).toBeNull();
    expect(within(blade).getByRole("button", { name: "Icon is inherited from av" })).toBeTruthy();

    // The stem states its own, so the box holds it and the inherited value sits
    // behind it as the placeholder: what an emptied box would take, not what
    // the row shows.
    const stem = within(blade).getByLabelText("Stem") as HTMLInputElement;
    expect(stem.value).toBe("lab");
    expect(stem.placeholder).toBe("stem-from-the-server");
  });
});
