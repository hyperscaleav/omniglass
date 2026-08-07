import "@testing-library/jest-dom/vitest";
import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Metrics from "./Metrics";
import { METRICS_KEY, type MetricRow } from "../lib/metric_types";
import { ME_KEY, type Me } from "../lib/auth";

// The Metrics page is a single FlatList over the /metric-types catalog: the
// numeric lane of the signal registry, the twin of the Properties page. A metric
// type carries the numeric facts the property lane never holds (unit, precision)
// and its data type is int|float. Official rows are read-only;
// custom rows are writable only when the caller holds metric_type:*. Data is
// seeded into the query cache so no server is needed.
const seed: MetricRow[] = [
  { name: "icmp-rtt-avg", data_type: "float", display_name: "ICMP RTT (avg)", unit: "ms", precision: 1, official: true },
  { name: "cpu-load", data_type: "float", display_name: "CPU load", unit: "percent", official: true },
  { name: "boot-count", data_type: "int", official: false },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };
// A role-scoped principal holding the resource the API stamps (metric_type),
// with no wildcard to blur a wrong gate string into a pass.
const cataloger: Me = {
  principal: { id: "u-cat", kind: "human" },
  human: { username: "cataloger" },
  permissions: ["metric_type:create", "metric_type:update", "metric_type:delete"],
  grants: [],
};

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...METRICS_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Metrics />
    </QueryClientProvider>
  ));
}

async function openBlade(rowText: string): Promise<HTMLElement> {
  fireEvent.click(screen.getByText(rowText));
  return waitFor(() => {
    const el = asides()[0];
    if (!el) throw new Error("no blade yet");
    return el as HTMLElement;
  });
}

describe("Metrics page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists the seeded metric types with their units", () => {
    mount();
    expect(screen.getByText("icmp-rtt-avg")).toBeInTheDocument();
    expect(screen.getByText("cpu-load")).toBeInTheDocument();
    expect(screen.getByText("boot-count")).toBeInTheDocument();
    const row = screen.getByText("icmp-rtt-avg").closest("tr")!;
    expect(within(row).getByText("ms")).toBeInTheDocument();
  });

  it("renders the display name above the name in one identity cell, no Label column", () => {
    mount();
    const cell = screen.getByText("icmp-rtt-avg").closest("td");
    expect(cell?.textContent).toContain("ICMP RTT (avg)");
    expect(screen.getByRole("columnheader", { name: "Name" })).toBeTruthy();
    expect(screen.queryByRole("columnheader", { name: "Label" })).toBeNull();
  });

  it("shows unit, precision, and data type on the blade; an official row is read-only", async () => {
    mount();
    const blade = await openBlade("ICMP RTT (avg)");
    expect(within(blade).getByText("float")).toBeInTheDocument();
    expect(within(blade).getByText("Unit")).toBeInTheDocument();
    expect(within(blade).getByText("ms")).toBeInTheDocument();
    expect(within(blade).getByText("Precision")).toBeInTheDocument();
    expect(within(blade).getByText("1")).toBeInTheDocument();
    // Official row: the Edit / Delete pair stays in the footer, greyed, with the
    // lock reason on each button's tooltip wrapper.
    const editBtn = within(blade).getByLabelText("Edit") as HTMLButtonElement;
    const deleteBtn = within(blade).getByRole("button", { name: /delete/i }) as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(deleteBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Official: ships with Omniglass and updates with it.");
  });

  it("lets a role-scoped metric_type principal edit a custom row", async () => {
    mount(cataloger);
    const blade = await openBlade("boot-count");
    expect(within(blade).getByLabelText("Edit")).toBeInTheDocument();
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByRole("button", { name: /delete/i })).toBeInTheDocument();
  });

  it("shows New metric for an admin and for a role-scoped metric_type:create holder", () => {
    mount(admin);
    expect(screen.getByText("New metric")).toBeInTheDocument();
  });

  it("shows New metric under the exact resource the API stamps (metric_type)", () => {
    mount(cataloger);
    expect(screen.getByText("New metric")).toBeInTheDocument();
  });

  it("hides New metric from a read-only viewer", () => {
    mount(viewer);
    expect(screen.queryByText("New metric")).toBeNull();
  });
});
