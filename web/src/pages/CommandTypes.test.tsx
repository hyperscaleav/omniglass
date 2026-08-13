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

// #718: the settle window is a decision a settleable type asks for, so the console
// must stop answering it. The form used to seed the field with "0" and fold it with
// `Number(settle()) || 0`, so a type naming a target arm was created with a window
// judged at the instant of issue that nobody typed. Zero itself stays legal
// (ADR-0108 makes it a statement of intent, and reboot ships it); what changes is
// that the operator has to state it.
describe("Command Types settle window", () => {
  afterEach(() => vi.restoreAllMocks());

  const openCreate = async () => {
    mount(admin);
    fireEvent.click(screen.getByText("New command type"));
    return await waitFor(() => screen.getByLabelText("Settle window (seconds)") as HTMLInputElement);
  };
  const submitBtn = () => screen.getByText("Create command type").closest("button") as HTMLButtonElement;
  const nameField = () => screen.getByLabelText("Name") as HTMLInputElement;
  const targetPicker = () => screen.getByLabelText("Target") as HTMLSelectElement;

  it("does not volunteer a window on the create form", async () => {
    const settle = await openCreate();
    expect(settle.value).toBe("");
  });

  it("refuses to create a settleable type until the window is stated", async () => {
    const settle = await openCreate();
    fireEvent.input(nameField(), { target: { value: "set-power" } });
    // No target arm yet: fire-and-forget, and nothing is owed.
    expect(submitBtn().disabled).toBe(false);
    fireEvent.change(targetPicker(), { target: { value: "property:audio-level" } });
    expect(submitBtn().disabled).toBe(true);
    expect(screen.getByText(/needs a settle window/i)).toBeTruthy();
    fireEvent.input(settle, { target: { value: "15" } });
    expect(submitBtn().disabled).toBe(false);
  });

  it("creates a fire-and-forget type with no window on the wire at all", async () => {
    let post: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST") {
        post = req.clone();
        return new Response(JSON.stringify({ name: "restart", settle_window_seconds: 0, official: false }), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response(JSON.stringify({ command_types: seed }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    await openCreate();
    fireEvent.input(nameField(), { target: { value: "restart" } });
    fireEvent.click(submitBtn());
    await waitFor(() => expect(post).toBeTruthy());
    expect(Object.keys((await post!.json()) as object)).not.toContain("settle_window_seconds");
  });

  it("accepts a stated zero beside a target and says what zero does there", async () => {
    const settle = await openCreate();
    fireEvent.input(nameField(), { target: { value: "set-power" } });
    fireEvent.change(targetPicker(), { target: { value: "property:audio-level" } });
    fireEvent.input(settle, { target: { value: "0" } });
    expect(submitBtn().disabled).toBe(false);
    expect(screen.getByText(/judged at the moment it is issued/i)).toBeTruthy();
  });

  // The server refuses a negative with a 422 (#718's first half). `min="0"` cannot
  // refuse it here: the drawer's action bar sits outside the <form>, so native
  // constraint validation never runs on the path the operator actually uses.
  it("refuses a negative window before the server does", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const settle = await openCreate();
    fireEvent.input(nameField(), { target: { value: "set-power" } });
    fireEvent.input(settle, { target: { value: "-5" } });
    expect(submitBtn().disabled).toBe(true);
    expect(screen.getByText(/cannot be negative/i)).toBeTruthy();
    fireEvent.click(submitBtn());
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("refuses to save a settleable type whose window has been cleared", async () => {
    mount(admin);
    fireEvent.click(screen.getByText("Set volume"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    const settle = within(blade).getByLabelText("Settle window (seconds)") as HTMLInputElement;
    expect(settle.value).toBe("5");
    const save = () => within(blade).getByText("Save").closest("button") as HTMLButtonElement;
    expect(save().disabled).toBe(false);
    fireEvent.input(settle, { target: { value: "" } });
    expect(save().disabled).toBe(true);
    expect(within(blade).getByText(/needs a settle window/i)).toBeTruthy();
  });
});
