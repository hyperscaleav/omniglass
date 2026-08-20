import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import AlarmsPanel from "./AlarmsPanel";
import { componentAlarmsKey, type Alarm } from "../lib/alarms";

// The alarms panel is where fleet health starts: a condition on this component
// that impairs its own verdict wholesale (#626). Rows are seeded into the query
// cache so no server is needed; the raise / clear writes are faked where a test
// drives one.
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

const alarms: Alarm[] = [
  {
    id: "a-1",
    component: "disp-1",
    severity: "warning",
    message: "Lamp hours exceeded",
    dedup_key: "test.condition",
    raised_at: ago(2 * 3_600_000),
    active: true,
    acknowledged: true,
    acknowledged_at: ago(90 * 60_000),
    acknowledged_by: "jordan",
  },
  {
    id: "a-2",
    component: "disp-1",
    severity: "critical",
    message: "HDMI board failed",
    dedup_key: "test.condition",
    raised_at: ago(3_600_000),
    active: true,
    acknowledged: false,
  },
  {
    id: "a-0",
    component: "disp-1",
    severity: "info",
    message: "Firmware mismatch",
    dedup_key: "test.condition",
    raised_at: ago(48 * 3_600_000),
    cleared_at: ago(24 * 3_600_000),
    active: false,
    acknowledged: false,
  },
];

function json(body: unknown, status = 200, type = "application/json") {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": type } });
}

function mount(opts: { rows?: Alarm[]; canUpdate?: boolean; canAcknowledge?: boolean } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...componentAlarmsKey("disp-1")], opts.rows ?? alarms);
  return render(() => (
    <QueryClientProvider client={qc}>
      <AlarmsPanel
        component="disp-1"
        canUpdate={opts.canUpdate ?? true}
        canAcknowledge={opts.canAcknowledge ?? true}
      />
    </QueryClientProvider>
  ));
}

const alarmRow = (label: HTMLElement) => label.closest("div.flex-col") as HTMLElement;

describe("AlarmsPanel", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists an active alarm with its severity and message", () => {
    const { getByText } = mount();
    const row = alarmRow(getByText("HDMI board failed"));
    expect(within(row).getByText("critical")).toBeTruthy();
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

  it("raises an alarm with its severity and message", async () => {
    let post: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST") { post = req.clone(); return json(alarms[1], 201); }
      return json({ component: "disp-1", alarms });
    });

    const { getByLabelText, getByText } = mount();
    fireEvent.change(getByLabelText("Alarm severity"), { target: { value: "critical" } });
    fireEvent.input(getByLabelText("Alarm message"), { target: { value: "Fan seized" } });
    fireEvent.click(getByText("Raise alarm"));

    await waitFor(() => expect(post).toBeTruthy());
    expect(post!.url).toContain("/components/disp-1/alarms");
    expect(JSON.parse(await post!.text())).toEqual({
      severity: "critical",
      message: "Fan seized",
    });
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

  // The server's refusal is shown as sent, never swallowed into a generic line.
  it("surfaces the server's refusal verbatim", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST") {
        return json({ title: "Unprocessable Entity", status: 422, detail: "severity must be info, warning, or critical" }, 422, "application/problem+json");
      }
      return json({ component: "disp-1", alarms });
    });

    const { getByText, getAllByRole, queryByText } = mount();
    fireEvent.click(getByText("Raise alarm"));

    const alert = await waitFor(() => getAllByRole("alert")[0]);
    expect(alert.textContent).toBe("severity must be info, warning, or critical");
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

  // The acknowledgement. It is a fact about a PERSON, orthogonal to whether the
  // alarm is still raised, so the panel has to say both things at once.
  it("marks an alarm nobody has looked at, and names whoever looked at the others", () => {
    const { getByText } = mount();
    const unseen = alarmRow(getByText("HDMI board failed"));
    expect(within(unseen).getByText("unacknowledged")).toBeTruthy();
    const seen = alarmRow(getByText("Lamp hours exceeded"));
    expect(within(seen).getByText(/seen by jordan/)).toBeTruthy();
    expect(within(seen).queryByText("unacknowledged")).toBeNull();
  });

  // The queue an operator actually works, at a glance: raised, and unseen. The
  // acknowledged-but-still-raised alarm is deliberately not in it, which is the
  // whole point of the two facts being independent.
  it("counts only the raised alarms nobody has looked at", () => {
    const { getByTitle } = mount();
    expect(getByTitle(/nobody has recorded seeing it yet/i).textContent).toBe("1 unacknowledged");
  });

  it("shows no unacknowledged counter once every raised alarm has been seen", () => {
    const seenRows = alarms.map((a) => ({ ...a, acknowledged: true, acknowledged_by: "jordan" }));
    const { queryByText } = mount({ rows: seenRows });
    expect(queryByText(/unacknowledged/)).toBeNull();
  });

  it("acknowledges through the alarm's own custom method", async () => {
    let post: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST") { post = req.clone(); return json({ ...alarms[1], acknowledged: true }); }
      return json({ component: "disp-1", alarms });
    });

    const { getByLabelText } = mount();
    fireEvent.click(getByLabelText("Acknowledge alarm a-2"));

    await waitFor(() => expect(post).toBeTruthy());
    expect(post!.url).toContain("/components/disp-1/alarms/a-2:acknowledge");
  });

  // Acknowledging is NOT an edit of the component, so it is not behind edit mode:
  // the server gates it on its own permission with its own scope, and edit mode
  // exists to guard the component's own data (ADR-0109).
  it("offers the acknowledgement outside edit mode, and only to a caller that holds it", () => {
    const viewing = mount({ canUpdate: false });
    expect(viewing.getByLabelText("Acknowledge alarm a-2")).toBeTruthy();
    expect(viewing.queryByLabelText("Clear alarm a-2")).toBeNull();
    viewing.unmount();

    const uncapable = mount({ canAcknowledge: false });
    expect(uncapable.queryByLabelText("Acknowledge alarm a-2")).toBeNull();
    // The indicator is a READ, so it stays: a caller who cannot acknowledge can
    // still see that nobody has.
    expect(uncapable.getByText("unacknowledged")).toBeTruthy();
  });

  // An alarm that came and went with nobody looking is the one worth spotting in
  // the history, so the cleared group says so rather than staying silent.
  it("says when a cleared alarm was never acknowledged", () => {
    const { getByRole } = mount();
    const group = getByRole("group", { name: /recently cleared/i });
    expect(within(group).getByText(/never acknowledged/)).toBeTruthy();
  });
});
