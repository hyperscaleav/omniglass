import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ReconciliationPanel from "./ReconciliationPanel";
import { RECONCILIATION_KEY, type Reconciliation } from "../lib/reconciliation";

// The panel is read-only; data is seeded into the query cache so no server is
// needed. Drift is the server's flag, rendered as-is.
const seed: Reconciliation = {
  component: "disp-1",
  properties: [
    { property: "firmware_version", want: "1.0.0", told: null, is: "2.0.0", drift: true },
    { property: "serial_number", want: "SN-1", told: null, is: "SN-1", drift: false },
  ],
};

function mount(data: Reconciliation = seed) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...RECONCILIATION_KEY(data.component)], data);
  return render(() => (
    <QueryClientProvider client={qc}>
      <ReconciliationPanel name={data.component} />
    </QueryClientProvider>
  ));
}

describe("ReconciliationPanel", () => {
  it("renders a want/is row per property with the values", () => {
    const { getByText } = mount();
    expect(getByText("firmware_version")).toBeTruthy();
    expect(getByText("serial_number")).toBeTruthy();
    expect(getByText("1.0.0")).toBeTruthy();
    expect(getByText("2.0.0")).toBeTruthy();
  });

  it("marks a drifting property and counts the drift", () => {
    const { getByText, getAllByText } = mount();
    expect(getByText(/1 drifting/)).toBeTruthy();
    expect(getAllByText("drift").length).toBe(1);
  });

  it("shows a placeholder for a value that is not set", () => {
    const { getAllByText } = mount();
    // both rows have a null told
    expect(getAllByText("not set").length).toBe(2);
  });

  it("shows the empty state when there is nothing to reconcile", () => {
    const { getByText } = mount({ component: "c2", properties: [] });
    expect(getByText(/no declared properties to reconcile/i)).toBeTruthy();
  });
});
