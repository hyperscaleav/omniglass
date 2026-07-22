import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ResolutionPanel from "./ResolutionPanel";
import { componentSystemsKey, type Member } from "../lib/members";
import { effectiveTagsKey, type ResolvedTag } from "../lib/resolution";

// The panel answers "why is this value what it is". A resolved value is the
// survivor of a cascade, so every candidate comes back and the panel has to show
// the winner, the tier it won from, and what it beat.
const tags: ResolvedTag[] = [
  // The system band wins over a location binding it shadows.
  { key: "environment", value: "prod", owner_kind: "system", owner_name: "boardroom-a", band: 2, depth: 0, winner: true },
  { key: "environment", value: "staging", owner_kind: "location", owner_name: "boardroom", band: 1, depth: 0, winner: false },
  { key: "owner", value: "av-team", owner_kind: "global", owner_name: "", band: 0, depth: 0, winner: true },
] as ResolvedTag[];

const shared: Member[] = [
  { system: "boardroom-a", component: "shared-bar", primary: true, system_count: 2 },
  { system: "boardroom-b", component: "shared-bar", primary: false, system_count: 2 },
] as Member[];

const solo: Member[] = [
  { system: "boardroom-a", component: "solo-bar", primary: true, system_count: 1 },
] as Member[];

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(opts: { members?: Member[]; rows?: ResolvedTag[]; name?: string } = {}) {
  const name = opts.name ?? "shared-bar";
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...componentSystemsKey(name)], opts.members ?? shared);
  qc.setQueryData([...effectiveTagsKey(name, "")], opts.rows ?? tags);
  return render(() => (
    <QueryClientProvider client={qc}>
      <ResolutionPanel component={name} />
    </QueryClientProvider>
  ));
}

describe("ResolutionPanel", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the winning value and the tier it came from", () => {
    const { getByText } = mount();
    expect(getByText("environment")).toBeTruthy();
    expect(getByText("prod")).toBeTruthy();
    expect(getByText(/from system boardroom-a/)).toBeTruthy();
  });

  // A value that looks wrong is usually a value that won from the wrong tier, so
  // what it beat is shown rather than hidden behind an expander.
  it("shows what the winner shadowed", () => {
    const { getByText } = mount();
    expect(getByText(/staging from location boardroom/)).toBeTruthy();
  });

  it("names the global tier for a value nothing overrode", () => {
    const { getByText } = mount();
    expect(getByText(/from global/)).toBeTruthy();
  });

  // The selector is why this panel exists, and it must appear only when there is
  // genuinely a choice to make.
  it("offers a system selector when the component serves more than one", () => {
    const { getByLabelText } = mount();
    const sel = getByLabelText("Resolve against") as HTMLSelectElement;
    expect([...sel.options].map((o) => o.value)).toContain("boardroom-b");
    expect(sel.options[0].textContent).toContain("its default");
  });

  // The single-system case is nearly every component and must not pay for the
  // shared one.
  it("offers no selector when the component serves exactly one system", () => {
    const { queryByLabelText, queryByText } = mount({ members: solo, name: "solo-bar" });
    expect(queryByLabelText("Resolve against")).toBeNull();
    expect(queryByText(/resolves differently for each/)).toBeNull();
  });

  it("re-resolves against the chosen system", async () => {
    const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      json({
        tags: [
          { key: "environment", value: "lab", owner_kind: "system", owner_name: "boardroom-b", band: 2, depth: 0, winner: true },
        ],
      }),
    );
    const { getByLabelText, findByText } = mount();
    fireEvent.change(getByLabelText("Resolve against"), { target: { value: "boardroom-b" } });
    await waitFor(() => expect(spy).toHaveBeenCalled());
    const arg = spy.mock.calls[0]?.[0] as Request | string;
    const url = typeof arg === "string" ? arg : arg.url;
    expect(url).toContain("system=boardroom-b");
    expect(await findByText("lab")).toBeTruthy();
  });

  it("says so plainly when nothing reaches the component", () => {
    const { getByText } = mount({ rows: [], members: solo, name: "solo-bar" });
    expect(getByText("No tags reach this component.")).toBeTruthy();
  });
});
