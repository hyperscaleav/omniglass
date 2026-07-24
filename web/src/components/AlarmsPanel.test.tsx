import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import AlarmsPanel from "./AlarmsPanel";
import { componentAlarmsKey, type Alarm } from "../lib/alarms";
import { CAPABILITIES_KEY, type Capability } from "../lib/capabilities";

// The alarms panel is where estate health starts: a condition on this component,
// and the capabilities it takes away. Rows are seeded into the query cache so no
// server is needed; the raise / clear writes are faked where a test drives one.
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

const alarms: Alarm[] = [
  {
    id: "a-1",
    component: "disp-1",
    severity: "warning",
    message: "Lamp hours exceeded",
    raised_at: ago(2 * 3_600_000),
    capabilities: ["display"],
    active: true,
  },
  {
    id: "a-2",
    component: "disp-1",
    severity: "critical",
    message: "HDMI board failed",
    raised_at: ago(3_600_000),
    capabilities: ["display", "hdmi-input"],
    active: true,
  },
  {
    id: "a-0",
    component: "disp-1",
    severity: "info",
    message: "Firmware mismatch",
    raised_at: ago(48 * 3_600_000),
    cleared_at: ago(24 * 3_600_000),
    capabilities: [],
    active: false,
  },
];

const catalog: Capability[] = [
  { id: "display", name: "display", display_name: "Display", official: true },
  { id: "hdmi-input", name: "hdmi-input", display_name: "HDMI input", official: true },
];

function json(body: unknown, status = 200, type = "application/json") {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": type } });
}

function mount(opts: { rows?: Alarm[]; canUpdate?: boolean } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...componentAlarmsKey("disp-1")], opts.rows ?? alarms);
  qc.setQueryData([...CAPABILITIES_KEY], catalog);
  return render(() => (
    <QueryClientProvider client={qc}>
      <AlarmsPanel component="disp-1" canUpdate={opts.canUpdate ?? true} />
    </QueryClientProvider>
  ));
}

const alarmRow = (label: HTMLElement) => label.closest("div.flex-col") as HTMLElement;

describe("AlarmsPanel", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists an active alarm with its severity, message, and what it degrades", () => {
    const { getByText } = mount();
    const row = alarmRow(getByText("HDMI board failed"));
    expect(within(row).getByText("critical")).toBeTruthy();
    expect(within(row).getByText("display")).toBeTruthy();
    expect(within(row).getByText("hdmi-input")).toBeTruthy();
    expect(within(row).getByText(/raised 1h ago/)).toBeTruthy();
  });

  it("puts the worst alarm first, since that is the one that explains the room", () => {
    const { getAllByText } = mount();
    const messages = getAllByText(/HDMI board failed|Lamp hours exceeded/).map((e) => e.textContent);
    expect(messages).toEqual(["HDMI board failed", "Lamp hours exceeded"]);
  });

  // Clearing keeps the row: what was wrong, and when, outlives the fix.
  it("keeps a cleared alarm in its own group rather than dropping it", () => {
    const { getByRole } = mount();
    const group = getByRole("group", { name: /recently cleared/i });
    expect(within(group).getByText("Firmware mismatch")).toBeTruthy();
    expect(within(group).getByText(/cleared 1d ago/)).toBeTruthy();
  });

  it("says plainly that nothing is wrong when there is no active alarm", () => {
    const { getByText } = mount({ rows: [] });
    expect(getByText(/this component has no active alarm/i)).toBeTruthy();
  });

  it("raises an alarm with its severity, message, and the capabilities it degrades", async () => {
    let post: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST") { post = req.clone(); return json(alarms[1], 201); }
      return json({ component: "disp-1", alarms });
    });

    const { getByLabelText, getByText } = mount();
    fireEvent.change(getByLabelText("Alarm severity"), { target: { value: "critical" } });
    fireEvent.input(getByLabelText("Alarm message"), { target: { value: "Fan seized" } });
    fireEvent.change(getByLabelText("Capability this alarm degrades"), { target: { value: "display" } });
    fireEvent.click(getByLabelText("Add capability to alarm"));
    fireEvent.click(getByText("Raise alarm"));

    await waitFor(() => expect(post).toBeTruthy());
    expect(post!.url).toContain("/components/disp-1/alarms");
    expect(JSON.parse(await post!.text())).toEqual({
      severity: "critical",
      message: "Fan seized",
      capabilities: ["display"],
    });
  });

  it("drops a capability from the draft before raising", () => {
    const { getByLabelText, queryByLabelText } = mount();
    fireEvent.change(getByLabelText("Capability this alarm degrades"), { target: { value: "display" } });
    fireEvent.click(getByLabelText("Add capability to alarm"));
    fireEvent.click(getByLabelText("Do not degrade display"));
    expect(queryByLabelText("Do not degrade display")).toBeNull();
  });

  it("clears an alarm through the component's alarm route", async () => {
    let del: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "DELETE") { del = req.clone(); return new Response(null, { status: 204 }); }
      return json({ component: "disp-1", alarms });
    });

    const { getByLabelText } = mount();
    fireEvent.click(getByLabelText("Clear alarm a-2"));

    await waitFor(() => expect(del).toBeTruthy());
    expect(del!.url).toContain("/components/disp-1/alarms/a-2");
  });

  // The server refuses an unknown capability with a 422 naming it. That message IS
  // the answer, so it is shown as sent rather than swallowed.
  it("surfaces the server's refusal verbatim", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST") {
        return json({ title: "Unprocessable Entity", status: 422, detail: 'unknown capability "hdmi-input"' }, 422, "application/problem+json");
      }
      return json({ component: "disp-1", alarms });
    });

    const { getByText, getAllByRole, queryByText } = mount();
    fireEvent.click(getByText("Raise alarm"));

    const alert = await waitFor(() => getAllByRole("alert")[0]);
    expect(alert.textContent).toBe('unknown capability "hdmi-input"');
    expect(queryByText("The operation failed.")).toBeNull();
  });

  // The page is read-only in view mode: the raise form and the clear affordance
  // appear only when the caller is in edit mode AND holds component:update.
  it("shows no raise or clear control when the caller cannot update the component", () => {
    const { getByText, queryByText, queryByLabelText } = mount({ canUpdate: false });
    expect(getByText("HDMI board failed")).toBeTruthy(); // the read still renders
    expect(queryByLabelText("Alarm severity")).toBeNull();
    expect(queryByLabelText("Alarm message")).toBeNull();
    expect(queryByText("Raise alarm")).toBeNull();
    expect(queryByLabelText("Clear alarm a-2")).toBeNull();
  });

  it("says an alarm naming no capability reaches no role", () => {
    const { getByText } = mount({ rows: [{ ...alarms[0], capabilities: [] }] });
    expect(getByText(/it reaches no role/i)).toBeTruthy();
  });
});
