import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Settings from "./Settings";
import { SETTINGS_KEY, type SettingsRead } from "../lib/settings";
import { ME_KEY, type Me } from "../lib/auth";

// The Settings page is a config over the settings engine read: one card per
// namespace, one row per key, each row carrying an editable control, a provenance
// badge, an optional lock chip, and a layer-stack expand. Data is seeded into the
// query cache so no server is needed; `>` grants settings:update so every write
// affordance is present.
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
  sources: { "ui.theme": "code", "ui.default_landing": "code" },
  locks: {},
};

describe("Settings page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders a namespace section with a key label and a provenance badge", async () => {
    mount(defaultRead);
    expect(await screen.findByText(/theme/i)).toBeTruthy(); // the key label
    expect(screen.getByText("ui")).toBeTruthy(); // the namespace section title
    expect(screen.getAllByText("Default").length).toBeGreaterThan(0); // code -> Default badge
  });

  it("renders a theme select of the two shipped themes for ui.theme", async () => {
    mount(defaultRead);
    const select = (await screen.findByLabelText("theme")) as HTMLSelectElement;
    expect(select.tagName).toBe("SELECT");
    expect(select.value).toBe("omniglass-dark");
    const options = Array.from(select.options).map((o) => o.value);
    expect(options).toEqual(["omniglass-dark", "omniglass-light"]);
  });

  it("shows a Set-in-console badge and a Save button after editing an overridden value", async () => {
    const read: SettingsRead = {
      values: { ui: { theme: "omniglass-light" } },
      sources: { "ui.theme": "global" },
      locks: {},
    };
    mount(read);
    // A global source reads as an override, so its Restore affordance is present.
    expect(await screen.findByText("Set in console")).toBeTruthy();
    expect(screen.getByText("Restore")).toBeTruthy();
    // Editing the value surfaces Save (the row is dirty).
    const select = (await screen.findByLabelText("theme")) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: "omniglass-dark" } });
    expect(await screen.findByText("Save")).toBeTruthy();
  });

  it("shows a lock chip and disables the control when a key is locked", async () => {
    const read: SettingsRead = {
      values: { ui: { theme: "omniglass-dark" } },
      sources: { "ui.theme": "global" },
      locks: { "ui.theme": "global" },
    };
    mount(read);
    expect(await screen.findByText("Locked")).toBeTruthy();
    const select = (await screen.findByLabelText("theme")) as HTMLSelectElement;
    expect(select.disabled).toBe(true);
  });

  it("patches the namespace with the edited key when Save is clicked", async () => {
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
    const select = (await screen.findByLabelText("theme")) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: "omniglass-light" } });
    fireEvent.click(await screen.findByText("Save"));
    await waitFor(() => expect(calls.length).toBe(1));
    expect(calls[0].url).toContain("/settings/ui");
    expect(calls[0].body).toEqual({ theme: "omniglass-light" });
  });

  it("hides every write affordance for a principal without settings:update", async () => {
    const viewer: Me = { principal: { id: "u-v", kind: "human" }, human: { username: "vi" }, permissions: ["ui:read"], grants: [] };
    mount({ values: { ui: { theme: "omniglass-light" } }, sources: { "ui.theme": "global" }, locks: {} }, viewer);
    expect(await screen.findByText(/theme/i)).toBeTruthy();
    expect(screen.queryByText("Restore all defaults")).toBeNull();
    expect(screen.queryByText("Restore section")).toBeNull();
    expect(screen.queryByText("Restore")).toBeNull();
  });
});
