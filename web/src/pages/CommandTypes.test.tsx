import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import CommandTypes from "./CommandTypes";
import { COMMAND_TYPES_KEY, type CommandTypeRow } from "../lib/command_types";
import { PROPERTIES_KEY, type PropertyRow } from "../lib/properties";
import { METRICS_KEY, type MetricRow } from "../lib/metric_types";
import { ME_KEY, type Me } from "../lib/auth";

// The Command Types page is a single FlatList over the /command-types catalog.
// Official types are read-only; a custom type is writable only when the caller holds
// command_type:create / command_type:update. Data is seeded into the query cache.
const seed: CommandTypeRow[] = [
  { name: "set-input", display_name: "Set input", target_property_type: "video-input", settle_window_seconds: 15, official: true },
  { name: "reboot", display_name: "Reboot", settle_window_seconds: 0, official: true },
  { name: "set-volume", display_name: "Set volume", target_property_type: "audio-level", settle_window_seconds: 5, official: false },
  // The other arm of the exclusive arc (#596): a metric-targeted type.
  { name: "set-gain", display_name: "Set gain", target_metric_type: "mic-gain", settle_window_seconds: 5, official: false },
];

// The target picker draws its menu from both classifier catalogs.
const properties: PropertyRow[] = [
  { name: "video-input", data_type: "string", display_name: "Video input", official: true },
  { name: "audio-level", data_type: "string", display_name: "Audio level", official: false },
];
const metrics: MetricRow[] = [
  { name: "mic-gain", data_type: "float", display_name: "Mic gain", official: false },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...COMMAND_TYPES_KEY], seed);
  qc.setQueryData([...PROPERTIES_KEY], properties);
  qc.setQueryData([...METRICS_KEY], metrics);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <CommandTypes />
    </QueryClientProvider>
  ));
}

describe("Command Types page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists the seeded command types with their target and settle window", () => {
    mount();
    expect(screen.getByText("set-input")).toBeTruthy();
    expect(screen.getByText("reboot")).toBeTruthy();
    expect(screen.getByText("video-input")).toBeTruthy();
    // reboot is fire-and-forget (no target).
    expect(screen.getByText("fire-and-forget")).toBeTruthy();
  });

  // One identity column carries both operator-facing identities, so the separate
  // label column is gone. The header is the one word every list uses, even though
  // this catalog's names may be dotted (set-input, icmp-rtt-avg) rather than kebab.
  it("carries the display name and the name in a single Name column", () => {
    mount();
    const headers = screen.getAllByRole("columnheader").map((h) => h.textContent?.trim());
    expect(headers).toContain("Name");
    expect(headers).not.toContain("Label");
    expect(screen.getByText("Set input")).toBeTruthy();
    expect(screen.getByText("set-input")).toBeTruthy();
  });

  it("shows New command type for a caller holding command_type:create", () => {
    mount(admin);
    expect(screen.getByText("New command type")).toBeTruthy();
  });

  it("hides New command type from a read-only viewer", () => {
    mount(viewer);
    expect(screen.queryByText("New command type")).toBeNull();
  });

  // The Target column shows whichever arm is set, with the lane named so a
  // property target and a metric target are distinguishable at a glance.
  // An official row keeps its footer: the Edit / Delete pair renders in the
  // usual spots, greyed, with the official reason on each button's tooltip
  // wrapper. The old in-body banner is gone; the reason lives on the buttons.
  it("an official row greys Edit and Delete with the official reason instead of a body banner", async () => {
    mount(admin);
    fireEvent.click(screen.getByText("set-input"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).queryByRole("alert")).toBeNull();
    const editBtn = within(blade).getByLabelText("Edit") as HTMLButtonElement;
    const deleteBtn = within(blade).getByText("Delete").closest("button") as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(deleteBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Official: ships with Omniglass and updates with it.");
    expect(deleteBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Official: ships with Omniglass and updates with it.");
    fireEvent.click(editBtn);
    expect(within(blade).queryByText("Save")).toBeNull();
  });

  it("shows a metric-armed target with its lane hint", () => {
    mount();
    expect(screen.getByText("mic-gain")).toBeTruthy();
    expect(screen.getAllByText("metric").length).toBeGreaterThan(0);
    expect(screen.getAllByText("property").length).toBeGreaterThan(0);
  });

  // The blade's edit mode authors the target on either arm: one picker over
  // both catalogs, one selection setting one arm.
  it("offers the target picker over both catalogs in the blade's edit mode", async () => {
    mount(admin);
    fireEvent.click(screen.getByText("Set volume"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    const picker = within(blade).getByRole("combobox");
    expect(within(picker).getByRole("group", { name: "Properties" })).toBeTruthy();
    expect(within(picker).getByRole("group", { name: "Metrics" })).toBeTruthy();
    // One selection sets one arm; the current target is the selected option.
    expect((picker as HTMLSelectElement).value).toBe("property:audio-level");
  });

  it("offers the target picker over both catalogs on the create form", async () => {
    mount(admin);
    fireEvent.click(screen.getByText("New command type"));
    const picker = await waitFor(() => screen.getByRole("combobox"));
    expect(within(picker).getByRole("group", { name: "Properties" })).toBeTruthy();
    expect(within(picker).getByRole("group", { name: "Metrics" })).toBeTruthy();
  });
});
