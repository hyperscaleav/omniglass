import { describe, it, expect } from "vitest";
import { render, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import SystemHealthPanel, { LocationHealthPanel } from "./HealthPanel";
import { locationHealthKey, systemHealthKey, type EstateHealth } from "../lib/health";

// The reconciliation panel is the claim this slice makes: it must answer "why is
// this degraded" in one view by naming the whole chain, alarm on a component ->
// degraded capability -> role below quorum -> verdict. Data is seeded into the
// query cache so no server is needed; the panel is read-only, so nothing is faked.
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

const degraded: EstateHealth = {
  owner: "boardroom",
  owner_kind: "system",
  verdict: "outage",
  systems: [],
  roles: [
    // Impaired by an alarm: a critical fault on disp-2 took the display capability
    // away, so the role holds 1 of the 2 it needs and takes the system out.
    {
      name: "main-display",
      display_name: "Main display",
      impact: "outage",
      impaired: true,
      quorum: 2,
      satisfying: 1,
      required: ["display", "hdmi-input"],
      degraded: ["display"],
      assigned_to: ["disp-1", "disp-2"],
      alarms: [
        {
          id: "a-2",
          severity: "critical",
          message: "HDMI board failed",
          component: "disp-2",
          raised_at: ago(3 * 3_600_000),
          capabilities: ["display"],
        },
      ],
    },
    // Impaired with no alarm: simply short of components.
    {
      name: "table-mic",
      display_name: "Table microphone",
      impact: "degraded",
      impaired: true,
      quorum: 2,
      satisfying: 0,
      required: ["microphone"],
      degraded: [],
      assigned_to: [],
      alarms: [],
    },
    // Holding, so it is named as such rather than left implicit.
    {
      name: "touch-panel",
      display_name: "Touch panel",
      impact: "degraded",
      impaired: false,
      quorum: 1,
      satisfying: 1,
      required: ["touch-panel"],
      degraded: [],
      assigned_to: ["panel-1"],
      alarms: [],
    },
  ],
  transitions: [
    { ts: ago(30 * 3_600_000), verdict: "healthy" },
    { ts: ago(5 * 3_600_000), verdict: "degraded" },
    { ts: ago(3 * 3_600_000), verdict: "outage" },
  ],
};

const healthy: EstateHealth = {
  owner: "huddle",
  owner_kind: "system",
  verdict: "healthy",
  systems: [],
  roles: [
    {
      name: "main-display",
      display_name: "Main display",
      impact: "outage",
      impaired: false,
      quorum: 1,
      satisfying: 1,
      required: ["display"],
      degraded: [],
      assigned_to: ["disp-9"],
      alarms: [],
    },
  ],
  transitions: [],
};

function mountSystem(data: EstateHealth, onOpenComponent?: (name: string) => void) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...systemHealthKey(data.owner)], data);
  return render(() => (
    <QueryClientProvider client={qc}>
      <SystemHealthPanel system={data.owner} onOpenComponent={onOpenComponent} />
    </QueryClientProvider>
  ));
}

// The impaired-role block is the flex column the role's technical name sits in
// (the display name also appears inside the chain, so the address is the anchor).
const roleBlock = (getByText: (t: string) => HTMLElement, name: string) =>
  getByText(name).closest("div.flex-col") as HTMLElement;

describe("SystemHealthPanel reconciliation", () => {
  it("carries the verdict as a word, not only a colour", () => {
    const { getAllByText } = mountSystem(degraded);
    expect(getAllByText("outage").length).toBeGreaterThan(0);
  });

  // The whole point of the slice: the panel must name the alarm that caused the
  // impairment and the role it impaired, in one view, or the verdict is just
  // another badge nobody trusts.
  it("names the causing alarm and the impaired role it took down", () => {
    const { getByText } = mountSystem(degraded);
    const block = roleBlock(getByText, "main-display");
    // The alarm: its message, its severity, and the component it is on.
    expect(within(block).getByText("HDMI board failed")).toBeTruthy();
    expect(within(block).getByText("critical")).toBeTruthy();
    expect(within(block).getAllByText("disp-2").length).toBeGreaterThan(0);
    // The role: impaired, and how far short of quorum it is.
    expect(within(block).getByText("impaired")).toBeTruthy();
    // Stated twice on purpose: once in the role header, once as the chain's link.
    expect(within(block).getAllByText("1 of 2 satisfying").length).toBe(2);
  });

  it("names the degraded capability as the link between the alarm and the role", () => {
    const { getByText } = mountSystem(degraded);
    const block = roleBlock(getByText, "main-display");
    // The chain step, and the requirement chip marked as taken away.
    expect(within(block).getByText("Capability degraded")).toBeTruthy();
    expect(within(block).getByText(/^display degraded$/)).toBeTruthy();
    // The capability the alarm did NOT touch is still listed, unmarked.
    expect(within(block).getByText("hdmi-input")).toBeTruthy();
  });

  it("spells the chain out in order: alarm, capability, role, contribution", () => {
    const { getByText } = mountSystem(degraded);
    const block = roleBlock(getByText, "main-display");
    const captions = within(block)
      .getAllByText(/Alarm on a component|Capability degraded|Role below quorum|Contributes/)
      .map((e) => e.textContent);
    expect(captions).toEqual(["Alarm on a component", "Capability degraded", "Role below quorum", "Contributes"]);
  });

  it("states the whole chain as one sentence, naming every link", () => {
    const { getByText } = mountSystem(degraded);
    expect(
      getByText(
        /A critical alarm on disp-2 degrades display, so Main display satisfies 1 of 2 and contributes outage, which is why this system reads outage\./,
      ),
    ).toBeTruthy();
  });

  it("separates a short-staffed role from a broken one", () => {
    const { getByText } = mountSystem(degraded);
    const block = roleBlock(getByText, "table-mic");
    expect(within(block).getByText(/No alarm reaches Table microphone/)).toBeTruthy();
    expect(within(block).getByText(/there are simply fewer of them than the quorum wants/)).toBeTruthy();
    expect(within(block).queryByText("Capability degraded")).toBeNull();
  });

  it("orders the impaired roles worst impact first, and names what is holding", () => {
    const { getByText, getAllByText } = mountSystem(degraded);
    expect(getByText("2 of 3 roles impaired, worst first.")).toBeTruthy();
    const names = getAllByText(/Main display|Table microphone/).map((e) => e.textContent);
    expect(names.indexOf("Main display")).toBeLessThan(names.indexOf("Table microphone"));
    // The roles that are fine are named, not left implicit.
    expect(getByText("holding")).toBeTruthy();
    expect(getByText(/Touch panel/)).toBeTruthy();
  });

  it("links an alarm's component when the caller offers a drill-down", () => {
    const opened: string[] = [];
    const { getByRole } = mountSystem(degraded, (name) => opened.push(name));
    // The alarm's component is the link; the same name also sits in the assigned
    // chips, which are not clickable.
    getByRole("button", { name: "disp-2" }).click();
    expect(opened).toEqual(["disp-2"]);
  });

  it("says a healthy system is healthy, plainly, and shows no chain", () => {
    const { getByRole, queryByText } = mountSystem(healthy);
    const status = getByRole("status");
    expect(status.textContent).toMatch(/This system is healthy/);
    expect(status.textContent).toMatch(/All 1 role it needs are filled and meet their quorum/);
    expect(queryByText("Capability degraded")).toBeNull();
    expect(queryByText("impaired")).toBeNull();
  });

  it("says there is nothing to reconcile when the system declares no roles", () => {
    const { getByText } = mountSystem({ ...healthy, owner: "bare", roles: [] });
    expect(getByText(/This system declares no roles/)).toBeTruthy();
  });
});

// The recorded edges: the durable requirement behind the design. An operator has to
// be able to read back, weeks later, when it changed and how long each state held.
describe("SystemHealthPanel history", () => {
  it("renders each recorded edge newest first, with how long it held", () => {
    const { getByText, getAllByText } = mountSystem(degraded);
    expect(getByText("History")).toBeTruthy();
    expect(getByText("2 changes recorded in this window.")).toBeTruthy();
    // The current state is open-ended; the ones before it are closed durations.
    expect(getAllByText(/and counting/).length).toBe(1);
    expect(getByText("held 2h")).toBeTruthy(); // degraded ran 5h ago -> 3h ago
    expect(getByText("held 1d 1h")).toBeTruthy(); // healthy ran 30h ago -> 5h ago
  });

  it("says so when nothing changed in the window, rather than drawing an empty bar", () => {
    const { getByText } = mountSystem(healthy);
    expect(getByText(/No change recorded in this window/)).toBeTruthy();
    expect(getByText("100% healthy")).toBeTruthy();
  });
});

const location: EstateHealth = {
  owner: "hq-floor-3",
  owner_kind: "location",
  verdict: "outage",
  roles: [],
  systems: [
    { name: "boardroom", verdict: "outage" },
    { name: "huddle", verdict: "healthy" },
  ],
  transitions: [{ ts: ago(2 * 3_600_000), verdict: "outage" }],
};

function mountLocation(onOpenSystem?: (name: string) => void) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...locationHealthKey(location.owner)], location);
  return render(() => (
    <QueryClientProvider client={qc}>
      <LocationHealthPanel location={location.owner} onOpenSystem={onOpenSystem} />
    </QueryClientProvider>
  ));
}

describe("LocationHealthPanel", () => {
  it("rolls up worst-wins and names the system that decided it", () => {
    const { getByText } = mountLocation();
    expect(getByText(/Worst of 2 systems beneath: boardroom reads outage, so this location reads outage\./)).toBeTruthy();
  });

  it("lists each system beneath with its own verdict as a word", () => {
    const { getByText } = mountLocation();
    const row = getByText("boardroom").parentElement as HTMLElement;
    expect(within(row).getByText("outage")).toBeTruthy();
    const ok = getByText("huddle").parentElement as HTMLElement;
    expect(within(ok).getByText("healthy")).toBeTruthy();
  });

  it("drills into the system that can say why", () => {
    const opened: string[] = [];
    const { getByText } = mountLocation((name) => opened.push(name));
    (getByText("boardroom").parentElement as HTMLElement).click();
    expect(opened).toEqual(["boardroom"]);
  });

  it("says there is nothing to roll up when no system sits beneath", () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...locationHealthKey("empty")], { ...location, owner: "empty", systems: [], verdict: "healthy" });
    const { getByText } = render(() => (
      <QueryClientProvider client={qc}>
        <LocationHealthPanel location="empty" />
      </QueryClientProvider>
    ));
    expect(getByText(/No system is placed beneath this location/)).toBeTruthy();
  });
});
