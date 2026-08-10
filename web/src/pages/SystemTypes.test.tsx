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
  { id: uuidFor("st-av"), name: "av", display_name: "AV", official: true, stem: "av", abbrev: "av", icon: "layers" },
  { id: uuidFor("st-room"), name: "room", display_name: "Room", official: true, parent: "av", parent_id: uuidFor("st-av"), stem: "room", abbrev: "rm", icon: "door-open" },
  { id: uuidFor("st-board"), name: "board", display_name: "Boardroom", official: true, parent: "room", parent_id: uuidFor("st-room"), stem: "boardroom", abbrev: "br" },
  { id: uuidFor("st-lab"), name: "lab", display_name: "Lab", official: false, parent: "room", parent_id: uuidFor("st-room"), stem: "lab", abbrev: "lab" },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

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

  it("shows a node's resolved icon, inherited from the nearest ancestor that sets one", () => {
    mount();
    const rows = screen.getAllByRole("row").slice(1);
    const boardRow = rows.find((r) => within(r).getAllByRole("cell")[0].textContent?.includes("Boardroom"))!;
    // board sets no icon of its own: the Icon cell names room's, not av's.
    expect(boardRow.textContent).toContain("door-open");
    expect(boardRow.textContent).not.toContain("layers");
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
    // An inherited fact must ride as OMITTED, never as "". The columns are
    // nullable and the server's walk treats only NULL as inherit while the
    // patch coalesces, so an empty string would write a real value that stops
    // the walk for this node and every descendant, silently and permanently.
    // Lab carries no icon of its own, so this is the inherited case.
    expect(sent).not.toHaveProperty("icon");
    expect(Object.values(sent as Record<string, unknown>)).not.toContain("");
  });
});
