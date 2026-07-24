import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Settings from "./Settings";
import { SETTINGS_KEY, type SettingsRead } from "../lib/settings";
import { ME_KEY, type Me } from "../lib/auth";

// The Settings page renders the settings-engine read through the shared KVRow
// primitive: one card per namespace, one KVRow per key. Read mode is a value scan
// with the origin weighted (no badge for the declared default, a neutral "Set in
// console" / "From settings file" badge otherwise) and a drill-in to the layer
// stack; an Edit toggle swaps in the controls with inline save and restore. Data
// is seeded into the query cache so no server is needed; `>` grants settings:update
// so every write affordance is present.
const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };

function mount(read: SettingsRead, principal: Me = me) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...SETTINGS_KEY], read);
  qc.setQueryData([...ME_KEY], principal);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <Settings />} />
      </Router>
    </QueryClientProvider>
  ));
}

const defaultRead: SettingsRead = {
  values: { ui: { theme: "omniglass-dark", default_landing: "/" } },
  sources: { "ui.theme": "default", "ui.default_landing": "default" },
  locks: {},
};

const enterEdit = () => fireEvent.click(screen.getByRole("button", { name: /edit/i }));

describe("Settings page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("read mode shows the namespace, key labels, and values with no badge for a declared default", async () => {
    mount(defaultRead);
    expect(await screen.findByText("ui")).toBeTruthy(); // namespace section title
    expect(screen.getByText("theme")).toBeTruthy(); // key label
    expect(screen.getByText("omniglass-dark")).toBeTruthy(); // value, inline in read mode
    // KVRow suppresses the origin badge for the declared default (weight, not a badge).
    expect(screen.queryByText("Default")).toBeNull();
    expect(screen.queryByText("Set in console")).toBeNull();
    // Read mode is a pure scan: no inputs and no Save until you enter Edit.
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("shows a neutral origin badge for an overridden value in read mode", async () => {
    mount({ values: { ui: { theme: "omniglass-light" } }, sources: { "ui.theme": "platform" }, locks: {} });
    expect(await screen.findByText("Set in console")).toBeTruthy();
  });

  it("reveals the theme select in edit mode and patches the namespace on Save", async () => {
    const calls: { url: string; method: string; body: unknown }[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input instanceof URL ? input.href : (input as Request).url;
      const method = (typeof input === "string" || input instanceof URL ? init?.method : (input as Request).method) ?? "GET";
      if (method === "PATCH") {
        const raw = typeof input === "string" || input instanceof URL ? (init?.body as string | undefined) : await (input as Request).clone().text();
        calls.push({ url, method, body: raw ? JSON.parse(raw) : null });
        return new Response(JSON.stringify(defaultRead), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify(defaultRead), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount(defaultRead);
    enterEdit();
    const select = (await screen.findByRole("combobox")) as HTMLSelectElement;
    expect(select.value).toBe("omniglass-dark");
    expect(Array.from(select.options).map((o) => o.value)).toEqual(["omniglass-dark", "omniglass-light"]);
    fireEvent.change(select, { target: { value: "omniglass-light" } });
    fireEvent.click(await screen.findByRole("button", { name: "Save" }));
    await waitFor(() => expect(calls.length).toBe(1));
    expect(calls[0].url).toContain("/settings/ui");
    expect(calls[0].body).toEqual({ theme: "omniglass-light" });
  });

  it("sources the theme select options from the generated schema", async () => {
    mount(defaultRead);
    enterEdit();
    const select = (await screen.findByRole("combobox")) as HTMLSelectElement;
    // The options come from settings.schema.gen.ts (the reflected enum), not a
    // hand-kept list, so a struct-tag change reflows the control.
    expect(Array.from(select.options).map((o) => o.value)).toEqual(["omniglass-dark", "omniglass-light"]);
  });

  it("shows an inline error and blocks Save for an out-of-enum value", async () => {
    mount(defaultRead);
    enterEdit();
    const select = (await screen.findByRole("combobox")) as HTMLSelectElement;
    // Drive the draft off the enum: an out-of-schema value must surface an inline
    // error and remove the Save affordance so the invalid write cannot be submitted.
    fireEvent.change(select, { target: { value: "purple" } });
    expect(await screen.findByText(/must be one of/i)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("marks a locked key and keeps it read-only even in edit mode", async () => {
    mount({ values: { ui: { theme: "omniglass-dark" } }, sources: { "ui.theme": "platform" }, locks: { "ui.theme": "platform" } });
    expect(await screen.findByText("Locked")).toBeTruthy();
    enterEdit();
    // The locked row stays in read mode, so no editable control appears for it.
    expect(screen.queryByRole("combobox")).toBeNull();
  });

  it("drills in to the layer stack from a read row", async () => {
    mount({ values: { ui: { theme: "omniglass-light" } }, sources: { "ui.theme": "platform" }, locks: {} });
    fireEvent.click(await screen.findByLabelText("Show resolution"));
    expect(await screen.findByText("Layer stack")).toBeTruthy();
    expect(screen.getByText("Console override (platform)")).toBeTruthy();
  });

  it("hides every write affordance for a principal without settings:update", async () => {
    const viewer: Me = { principal: { id: "u-v", kind: "human" }, human: { username: "vi" }, permissions: ["ui:read"], grants: [] };
    mount({ values: { ui: { theme: "omniglass-light" } }, sources: { "ui.theme": "platform" }, locks: {} }, viewer);
    expect(await screen.findByText("theme")).toBeTruthy();
    expect(screen.queryByText("Restore all defaults")).toBeNull();
    expect(screen.queryByRole("button", { name: /edit/i })).toBeNull();
    // No settings:update, so there is no half-held state to explain.
    expect(screen.queryByText(/platform:update/)).toBeNull();
  });

  // A settings write lands at the platform tier, so the server gates it on
  // platform:update on top of settings:update. The console gates on both, and says
  // which half is missing rather than letting the operator earn a 403 on Save.
  it("hides the write affordances and names the missing capability without platform:update", async () => {
    const settingsOnly: Me = { principal: { id: "u-s", kind: "human" }, human: { username: "sam" }, permissions: ["settings:read,update"], grants: [] };
    mount({ values: { ui: { theme: "omniglass-light" } }, sources: { "ui.theme": "platform" }, locks: {} }, settingsOnly);
    expect(await screen.findByText("theme")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /edit/i })).toBeNull();
    expect(screen.queryByText("Restore all defaults")).toBeNull();
    expect(screen.getByText(/platform:update/)).toBeTruthy();
  });

  it("keeps the write affordances when settings:update is paired with platform:update", async () => {
    const both: Me = { principal: { id: "u-a", kind: "human" }, human: { username: "ada" }, permissions: ["settings:read,update", "platform:update"], grants: [] };
    mount(defaultRead, both);
    expect(await screen.findByRole("button", { name: /edit/i })).toBeTruthy();
    expect(screen.getByText("Restore all defaults")).toBeTruthy();
    // Both halves held, so the explanation stays out of the way.
    expect(screen.queryByText(/platform:update/)).toBeNull();
  });
});
