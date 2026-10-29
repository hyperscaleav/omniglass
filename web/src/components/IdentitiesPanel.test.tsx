import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import IdentitiesPanel from "./IdentitiesPanel";
import {
  identityBindingsKey,
  unboundIdentitiesKey,
  type IdentityBindings,
  type UnboundIdentities,
} from "../lib/identity_bindings";
import { COMPONENTS_KEY, type Component as Comp } from "../lib/components";
import { SYSTEMS_KEY, type System } from "../lib/systems";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// The Identities panel on a conduit component's page (#1389): the identities
// it fans out (each bound value and the owner it lands on), the ones it heard
// that nothing places, and binding one in place. Reads are seeded into the
// query cache; writes are faked at fetch, and a write's refetch answers with
// the server's state after it, so a test sees the row move the way an
// operator does.
const CONDUIT = uuidFor("ucc-platform");
const now = Date.now();
const ago = (s: number) => new Date(now - s * 1000).toISOString();

const binding = (value: string, owner: string, kind: "system" | "component" = "system") => ({
  conduit: "ucc-platform",
  conduit_id: CONDUIT,
  key: "ucc-room-id",
  value,
  owner_kind: kind,
  owner_name: owner,
  owner_id: uuidFor(owner),
  created_at: ago(3600),
});

const bound: IdentityBindings = {
  conduit: CONDUIT,
  bindings: [binding("zr-boardroom-a", "boardroom-a"), binding("zr-huddle-north", "huddle-north")],
  hidden: 2,
};
const annex = {
  key: "ucc-room-id",
  value: "zr-annex-1",
  count: 12,
  first_heard_at: ago(600),
  last_heard_at: ago(40),
  keys_carried: ["meeting-active", "participant-count"],
};
const inbox: UnboundIdentities = { component: CONDUIT, identities: [annex] };

const systems: System[] = [
  { id: uuidFor("boardroom-a"), name: "boardroom-a", label: "Boardroom A", member_count: 2, actions: ["update"] },
  { id: uuidFor("huddle-north"), name: "huddle-north", label: "Huddle North", member_count: 2, actions: ["update"] },
  { id: uuidFor("annex"), name: "annex", label: "Annex", member_count: 1, actions: ["update"] },
];
const components: Comp[] = [
  { id: CONDUIT, name: "ucc-platform", label: "UCC platform", system_count: 0, actions: ["update"] },
  { id: uuidFor("panel-1"), name: "panel-1", label: "Panel 1", system_count: 1, actions: ["update"] },
];
const admin: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(opts: { bindings?: IdentityBindings; unbound?: UnboundIdentities; canUpdate?: boolean; unseeded?: boolean; systems?: System[] } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  if (!opts.unseeded) {
    qc.setQueryData([...identityBindingsKey(CONDUIT)], opts.bindings ?? bound);
    qc.setQueryData([...unboundIdentitiesKey(CONDUIT)], opts.unbound ?? inbox);
  }
  qc.setQueryData([...SYSTEMS_KEY], opts.systems ?? systems);
  qc.setQueryData([...COMPONENTS_KEY], components);
  qc.setQueryData([...ME_KEY], admin);
  const result = render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <IdentitiesPanel component={CONDUIT} canUpdate={opts.canUpdate ?? true} />} />
      </Router>
    </QueryClientProvider>
  ));
  return { ...result, qc };
}

const boundList = (q: ReturnType<typeof mount>) => q.getByTestId("identity-bindings");
const unboundList = (q: ReturnType<typeof mount>) => q.getByTestId("identity-unbound");

describe("IdentitiesPanel", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("renders nothing on a component that fans nothing out and heard nothing", () => {
    const { queryByTestId } = mount({ bindings: { conduit: CONDUIT, bindings: [], hidden: 0 }, unbound: { component: CONDUIT, identities: [] } });
    expect(queryByTestId("leaf-identities")).toBeNull();
  });

  it("lists each binding with its owner linked, and counts the owners the caller cannot see", () => {
    const q = mount();
    const list = boundList(q);
    expect(within(list).getByText("zr-boardroom-a")).toBeTruthy();
    const owner = within(list).getByRole("link", { name: "Boardroom A" });
    expect(owner.getAttribute("href")).toBe(`/systems/${uuidFor("boardroom-a")}`);
    expect(within(list).getAllByText("ucc-room-id").length).toBe(2);
    expect(within(list).getByText("and 2 more you cannot see")).toBeTruthy();
  });

  it("links a component owner to the component's page", () => {
    const q = mount({ bindings: { conduit: CONDUIT, bindings: [binding("amp-ch-1", "panel-1", "component")], hidden: 0 } });
    expect(within(boundList(q)).getByRole("link", { name: "Panel 1" }).getAttribute("href")).toBe(`/components/${uuidFor("panel-1")}`);
  });

  it("lists an unbound identity with its count, last heard and the keys it carried", () => {
    const q = mount();
    const row = within(unboundList(q)).getByTestId("unbound-ucc-room-id-zr-annex-1");
    expect(within(row).getByText("zr-annex-1")).toBeTruthy();
    expect(within(row).getByText("heard 12 times")).toBeTruthy();
    expect(within(row).getByText(/40s ago/)).toBeTruthy();
    expect(within(row).getByText("meeting-active")).toBeTruthy();
    expect(within(row).getByText("participant-count")).toBeTruthy();
  });

  it("says when every identity it heard has a home", () => {
    const q = mount({ unbound: { component: CONDUIT, identities: [] } });
    expect(q.getByText("Every identity it reported has an owner.")).toBeTruthy();
  });

  it("says when nothing is bound yet but values are waiting", () => {
    const q = mount({ bindings: { conduit: CONDUIT, bindings: [], hidden: 0 } });
    expect(q.getByText("Nothing bound yet.")).toBeTruthy();
    expect(within(unboundList(q)).getByText("zr-annex-1")).toBeTruthy();
  });

  it("binds an unbound identity in place, and the row moves to the bindings", async () => {
    let put: { url: string; body: unknown } | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        put = { url: req.url, body: await req.json() };
        return json(binding("zr-annex-1", "annex"));
      }
      if (req.url.includes("/unbound-identities")) return json({ component: CONDUIT, identities: [] });
      if (req.url.includes("/identity-bindings")) return json({ ...bound, bindings: [...bound.bindings!, binding("zr-annex-1", "annex")] });
      return json({});
    });
    const q = mount();
    fireEvent.click(q.getByRole("button", { name: "Bind zr-annex-1" }));
    const picker = q.getByLabelText("Owner for zr-annex-1") as HTMLSelectElement;
    // Only owners the caller may update, never the conduit itself.
    const values = Array.from(picker.options).map((o) => o.value);
    expect(values).toContain(`system:${uuidFor("annex")}`);
    expect(values).toContain(`component:${uuidFor("panel-1")}`);
    expect(values).not.toContain(`component:${CONDUIT}`);

    fireEvent.change(picker, { target: { value: `system:${uuidFor("annex")}` } });
    fireEvent.click(q.getByRole("button", { name: "Bind zr-annex-1 to the picked owner" }));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain(`/components/${CONDUIT}/identity-bindings/ucc-room-id/zr-annex-1`);
    expect(put!.body).toEqual({ owner_kind: "system", owner: uuidFor("annex") });
    await waitFor(() => expect(within(boundList(q)).getByRole("link", { name: "Annex" })).toBeTruthy());
    expect(q.queryByTestId("unbound-ucc-room-id-zr-annex-1")).toBeNull();
    expect(q.getByText("Every identity it reported has an owner.")).toBeTruthy();
  });

  it("shows a refused bind in plain words and keeps the row where it was", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        return json({ status: 403, title: "Forbidden", detail: "Ask for system:update over the system." }, 403);
      }
      if (req.url.includes("/unbound-identities")) return json(inbox);
      return json(bound);
    });
    const q = mount();
    fireEvent.click(q.getByRole("button", { name: "Bind zr-annex-1" }));
    fireEvent.change(q.getByLabelText("Owner for zr-annex-1"), { target: { value: `system:${uuidFor("annex")}` } });
    fireEvent.click(q.getByRole("button", { name: "Bind zr-annex-1 to the picked owner" }));

    const alert = await q.findByRole("alert");
    expect(alert.textContent).toContain("Not allowed: Ask for system:update over the system.");
    expect(q.getByTestId("unbound-ucc-room-id-zr-annex-1")).toBeTruthy();
  });

  it("reads the owners again when the one picked is gone", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") return json({ status: 404, title: "Not Found", detail: "owner system not found" }, 404);
      if (req.url.includes("/unbound-identities")) return json(inbox);
      if (req.url.includes("/identity-bindings")) return json(bound);
      if (req.url.includes("/systems")) return json({ systems: systems.slice(0, 2) });
      return json({ components });
    });
    const q = mount();
    const spy = vi.spyOn(q.qc, "invalidateQueries");
    fireEvent.click(q.getByRole("button", { name: "Bind zr-annex-1" }));
    fireEvent.change(q.getByLabelText("Owner for zr-annex-1"), { target: { value: `system:${uuidFor("annex")}` } });
    fireEvent.click(q.getByRole("button", { name: "Bind zr-annex-1 to the picked owner" }));

    expect((await q.findByRole("alert")).textContent).toContain("Not found: the owner is gone, or outside what you can see.");
    await waitFor(() => expect(spy.mock.calls.map((c) => JSON.stringify((c[0] as { queryKey: unknown }).queryKey))).toContain(JSON.stringify(SYSTEMS_KEY)));
  });

  it("removes a binding after a confirm, and the identity reads as unbound again", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    let del: string | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "DELETE") {
        del = req.url;
        return new Response(null, { status: 204 });
      }
      if (req.url.includes("/unbound-identities")) return json({ component: CONDUIT, identities: [annex, { ...annex, value: "zr-boardroom-a", count: 1 }] });
      return json({ ...bound, bindings: [bound.bindings![1]] });
    });
    const q = mount();
    fireEvent.click(q.getByRole("button", { name: "Unbind zr-boardroom-a" }));
    expect(window.confirm).toHaveBeenCalled();
    await waitFor(() => expect(del).toBeTruthy());
    expect(del).toContain(`/components/${CONDUIT}/identity-bindings/ucc-room-id/zr-boardroom-a`);
    await waitFor(() => expect(q.getByTestId("unbound-ucc-room-id-zr-boardroom-a")).toBeTruthy());
    expect(within(boundList(q)).queryByText("zr-boardroom-a")).toBeNull();
  });

  it("does nothing when the remove is not confirmed", () => {
    vi.spyOn(window, "confirm").mockReturnValue(false);
    const spy = vi.spyOn(globalThis, "fetch");
    const q = mount();
    fireEvent.click(q.getByRole("button", { name: "Unbind zr-boardroom-a" }));
    expect(spy).not.toHaveBeenCalled();
  });

  it("offers no bind or unbind to a caller who cannot update the conduit", () => {
    const q = mount({ canUpdate: false });
    expect(q.queryByRole("button", { name: "Bind zr-annex-1" })).toBeNull();
    expect(q.queryByRole("button", { name: "Unbind zr-boardroom-a" })).toBeNull();
    expect(within(unboundList(q)).getByText("zr-annex-1")).toBeTruthy();
  });
  // Both reads keep polling whichever answers first (review finding 1): the
  // poll decision is one reactive fact over both reads, so the read that
  // landed while the other was still out learns when the other lands.
  const pollsBoth = async (first: "bindings" | "unbound", b: IdentityBindings, u: UnboundIdentities) => {
    vi.useFakeTimers();
    const hits = { bindings: 0, unbound: 0 };
    let releaseSecond: (() => void) | undefined;
    const second = new Promise<void>((r) => (releaseSecond = r));
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = (input as Request).url;
      const which = url.includes("/unbound-identities") ? "unbound" : url.includes("/identity-bindings") ? "bindings" : null;
      if (!which) return json({});
      hits[which]++;
      if (which !== first && hits[which] === 1) await second;
      return json(which === "bindings" ? b : u);
    });
    mount({ unseeded: true });
    await vi.advanceTimersByTimeAsync(50);
    releaseSecond!();
    await vi.advanceTimersByTimeAsync(50);
    expect(hits).toEqual({ bindings: 1, unbound: 1 });
    await vi.advanceTimersByTimeAsync(5_100);
    expect(hits.bindings).toBeGreaterThanOrEqual(2);
    expect(hits.unbound).toBeGreaterThanOrEqual(2);
  };

  it("keeps polling the inbox when it answered empty before the bindings did", () =>
    pollsBoth("unbound", bound, { component: CONDUIT, identities: [] }));

  it("keeps polling the bindings when they answered empty before the inbox did", () =>
    pollsBoth("bindings", { conduit: CONDUIT, bindings: [], hidden: 0 }, inbox));

  it("offers no unbind on a binding whose owner the caller cannot update, or cannot see listed", () => {
    const q = mount({
      bindings: { conduit: CONDUIT, bindings: [binding("zr-boardroom-a", "boardroom-a"), binding("zr-x", "elsewhere")], hidden: 0 },
      systems: [{ ...systems[0], actions: ["read"] }, systems[1], systems[2]],
    });
    expect(q.queryByRole("button", { name: "Unbind zr-boardroom-a" })).toBeNull();
    expect(q.queryByRole("button", { name: "Unbind zr-x" })).toBeNull();
  });

  it("says an unbind of a binding already gone in its own words, and reads the lists again", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "DELETE") return json({ status: 404, title: "Not Found", detail: "no binding of this identity on this conduit" }, 404);
      if (req.url.includes("/unbound-identities")) return json(inbox);
      return json(bound);
    });
    const q = mount();
    const spy = vi.spyOn(q.qc, "invalidateQueries");
    fireEvent.click(q.getByRole("button", { name: "Unbind zr-boardroom-a" }));
    expect((await q.findByRole("alert")).textContent).toContain("This binding is already gone, or its owner is outside what you can see.");
    await waitFor(() => expect(spy.mock.calls.map((c) => JSON.stringify((c[0] as { queryKey: unknown }).queryKey))).toContain(JSON.stringify(identityBindingsKey(CONDUIT))));
  });

  it("ties the Bind button to its picker by an id that survives a value with spaces", () => {
    const q = mount({ unbound: { component: CONDUIT, identities: [{ ...annex, value: "zr room 1" }] } });
    const trigger = q.getByRole("button", { name: "Bind zr room 1" });
    fireEvent.click(trigger);
    const controls = trigger.getAttribute("aria-controls")!;
    expect(controls).not.toMatch(/\s/);
    expect(document.getElementById(controls)).toBe(q.getByTestId("bind-row-zr room 1"));
  });
});
