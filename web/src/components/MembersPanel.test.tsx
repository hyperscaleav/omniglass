import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import MembersPanel from "./MembersPanel";
import { systemMembersKey, type Member } from "../lib/members";
import { COMPONENTS_KEY, type Component as Comp } from "../lib/components";

// The panel answers "what is in this system". Membership is the attachment and a
// role is what it does, so a member may hold no role at all, and a member may
// belong to more than one system. Rows are seeded into the query cache so no
// server is needed; the writes are faked where a test drives one.
const members: Member[] = [
  // Its default lives here: this is the ordinary single-system case.
  { system: "boardroom-a", component: "boardroom-a-bar", primary: true, system_count: 1 },
  // The shared device, and the case that matters: its default IS here, and it
  // still serves another system. Sharing cannot be read off the default flag.
  { system: "boardroom-a", component: "shared-bar", primary: true, system_count: 2 },
  // A member with no role at all, which is the case staffing alone cannot produce.
  { system: "boardroom-a", component: "boardroom-power", primary: true, system_count: 1 },
];

const components: Comp[] = [
  { id: "c-1", name: "boardroom-a-bar", display_name: "Boardroom A Bar" },
  { id: "c-2", name: "shared-bar", display_name: "Shared Room Bar" },
  { id: "c-3", name: "boardroom-power", display_name: "Power Conditioner" },
  { id: "c-4", name: "spare-panel", display_name: "Spare Panel" },
] as Comp[];

// 204 carries no body: the Response constructor rejects one, exactly as the real
// no-content writes return.
function json(body: unknown, status = 200) {
  const noBody = status === 204 || status === 205 || status === 304;
  return new Response(noBody ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function mount(opts: { rows?: Member[]; canUpdate?: boolean } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...systemMembersKey("boardroom-a")], opts.rows ?? members);
  qc.setQueryData([...COMPONENTS_KEY], components);
  return render(() => (
    <QueryClientProvider client={qc}>
      <MembersPanel system="boardroom-a" canUpdate={opts.canUpdate ?? true} />
    </QueryClientProvider>
  ));
}

const memberRow = (label: HTMLElement) => label.closest("div.flex-col") as HTMLElement;

describe("MembersPanel", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists every component in the system, including one holding no role", () => {
    const { getByText } = mount();
    expect(getByText("boardroom-a-bar")).toBeTruthy();
    expect(getByText("shared-bar")).toBeTruthy();
    // The power conditioner fills no role anywhere. A staffing-only view would
    // lose it entirely, which is the reason membership is its own relation.
    expect(getByText("boardroom-power")).toBeTruthy();
  });

  // The shared device is the case a single pointer could never express, so the
  // panel has to say it out loud rather than let an operator assume this system
  // has the component to itself.
  it("says when a member is shared, even when its default is this system", () => {
    const { getByText, queryByText } = mount();
    const shared = memberRow(getByText("shared-bar"));
    expect(shared.textContent).toContain("shared with 1 other system");
    // A component in this system alone says nothing extra.
    const own = memberRow(getByText("boardroom-a-bar"));
    expect(own.textContent).not.toContain("shared with");
    expect(queryByText("No components in this system yet.")).toBeNull();
  });

  it("says so plainly when the system holds nothing", () => {
    const { getByText } = mount({ rows: [] });
    expect(getByText("No components in this system yet.")).toBeTruthy();
  });

  // View is read-only per the console invariant: the writes appear only in edit.
  it("offers no write controls unless the caller can update", () => {
    const { queryByLabelText } = mount({ canUpdate: false });
    expect(queryByLabelText("Component to add")).toBeNull();
    expect(queryByLabelText("Remove shared-bar")).toBeNull();
  });

  // The picker must not offer a component that is already here, which would be a
  // no-op, but must still offer one serving another system: that is the shared
  // device, and adding it is the point of the relation being many-valued.
  it("offers only components not already in this system", () => {
    const { getByLabelText } = mount();
    const opts = [...(getByLabelText("Component to add") as HTMLSelectElement).options].map((o) => o.value);
    expect(opts).toContain("spare-panel");
    expect(opts).not.toContain("boardroom-a-bar");
  });

  // The refusal is the lesson. The server explains that the component still fills
  // a role and must be unassigned first, and a generic failure would throw away
  // the only thing the operator needs.
  it("surfaces the server's refusal verbatim when a member still fills a role", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      json({ detail: "component still fills a role in this system; unassign the role first" }, 409),
    );
    const { getByLabelText, findByText } = mount();
    fireEvent.click(getByLabelText("Remove shared-bar"));
    expect(await findByText(/unassign the role first/)).toBeTruthy();
  });

  it("adds a component and refreshes", async () => {
    const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(json({}, 204));
    const { getByLabelText, getByText } = mount();
    fireEvent.change(getByLabelText("Component to add"), { target: { value: "spare-panel" } });
    fireEvent.click(getByText("Add"));
    await waitFor(() => expect(spy).toHaveBeenCalled());
    // openapi-fetch calls fetch with a Request, not a bare URL string.
    const arg = spy.mock.calls[0]?.[0] as Request | string;
    const url = typeof arg === "string" ? arg : arg.url;
    expect(url).toContain("/systems/boardroom-a/members/spare-panel");
  });
});
