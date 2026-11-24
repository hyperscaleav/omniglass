import "@testing-library/jest-dom/vitest";
import { describe, it, expect, afterEach } from "vitest";
import { render, cleanup } from "@solidjs/testing-library";
import { Show, createSignal } from "solid-js";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import AlarmActions from "./AlarmActions";
import { SendCommandHost } from "./SendCommandButton";
import { ME_KEY, type Me } from "../lib/auth";
import { ENDPOINTS_KEY, type Endpoint } from "../lib/endpoints";
import { DRIVERS_KEY, type Driver } from "../lib/drivers";
import { COMMAND_TYPES_KEY } from "../lib/command_types";
import { PROPERTIES_KEY } from "../lib/properties";
import { METRICS_KEY } from "../lib/metric_types";
import { uuidFor } from "../lib/testids";
import { fireEvent, screen } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";

const me = (permissions: string[]): Me => ({ user: { id: "p", kind: "person" }, person: { username: "ada" }, permissions, grants: [] });
const alarm = { id: "a-1", message: "no signal", component: "cam-1", raised_at: "2026-11-23T00:00:00Z" };

const driver = (commands: string[]) =>
  [{ id: uuidFor("lumen-drv"), name: "lumen-http", label: "Lumen", official: true, spec: { version: 1, transport: "https", commands: commands.map((c) => ({ command_type: c, request: {} })) } }] as Driver[];
const endpoints = [{ id: uuidFor("lumen-ep"), name: "http", component_id: "u-1", driver: "lumen-http" }] as Endpoint[];

function mount(who: Me, show: Parameters<typeof AlarmActions>[0]["show"], drivers: Driver[] = driver(["set-power"])) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData([...ME_KEY], who);
  qc.setQueryData([...ENDPOINTS_KEY], endpoints);
  qc.setQueryData([...DRIVERS_KEY], drivers);
  qc.setQueryData([...COMMAND_TYPES_KEY], [{ name: "set-power", settle_window_seconds: 15, target_property_type: "power-state", official: true }]);
  qc.setQueryData([...PROPERTIES_KEY], [{ name: "power-state", data_type: "string", validation: { enum: ["on", "off", "standby"] }, official: true }]);
  qc.setQueryData([...METRICS_KEY], []);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <><AlarmActions componentId="u-1" alarm={alarm} show={show} /><SendCommandHost /></>} />
      </Router>
    </QueryClientProvider>
  ));
}

afterEach(cleanup);

describe("AlarmActions", () => {
  // The group is not drawn when no member is: an empty flex item would still
  // take a gap in its row.
  it("draws no group for a caller who may use none of its members", () => {
    const { container } = mount(me(["component:read"]), { ack: true, snooze: true, troubleshoot: true });
    expect(container.querySelector("[data-alarm-actions]")).toBeNull();
  });

  it("draws the group once one member is allowed", () => {
    const { container, getByRole } = mount(me(["alarm:acknowledge"]), { ack: true, snooze: true });
    expect(container.querySelector("[data-alarm-actions]")).not.toBeNull();
    expect(getByRole("button", { name: "Acknowledge" })).toBeInTheDocument();
  });

  // Send command (#2466): the command form with the component filled in.
  it("offers Send command to a caller who may issue, where a driver binds a command", async () => {
    const { container } = mount(me(["command:issue"]), { command: true });
    const group = container.querySelector("[data-alarm-actions]");
    expect(group).not.toBeNull();
    fireEvent.click(await screen.findByRole("button", { name: "Send command" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain("Issue a command to cam-1");
    // One command is bound, so it is chosen, and its value is the select.
    const value = (await screen.findByLabelText("Value")) as HTMLSelectElement;
    expect([...value.options].map((o) => o.value).filter(Boolean)).toEqual(["on", "off", "standby"]);
  });

  // A list re-keys its rows on each poll; the form is not in the row, so it
  // (and what was typed in it) outlives the row that opened it.
  it("keeps the form open when the row that opened it is gone", async () => {
    const [shown, setShown] = createSignal(true);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    qc.setQueryData([...ME_KEY], me(["command:issue"]));
    qc.setQueryData([...ENDPOINTS_KEY], endpoints);
    qc.setQueryData([...DRIVERS_KEY], driver(["set-power"]));
    qc.setQueryData([...COMMAND_TYPES_KEY], [{ name: "set-power", settle_window_seconds: 15, target_property_type: "power-state", official: true }]);
    qc.setQueryData([...PROPERTIES_KEY], []);
    qc.setQueryData([...METRICS_KEY], []);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="*" component={() => <><Show when={shown()}><AlarmActions componentId="u-1" alarm={alarm} show={{ command: true }} /></Show><SendCommandHost /></>} />
        </Router>
      </QueryClientProvider>
    ));
    fireEvent.click(await screen.findByRole("button", { name: "Send command" }));
    await screen.findByRole("dialog");
    setShown(false);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("draws no Send command without the permission or without a command to send", () => {
    mount(me(["alarm:acknowledge"]), { ack: true, command: true });
    expect(screen.queryByRole("button", { name: "Send command" })).toBeNull();
    cleanup();
    const { container } = mount(me(["command:issue"]), { command: true }, driver([]));
    expect(screen.queryByRole("button", { name: "Send command" })).toBeNull();
    expect(container.querySelector("[data-alarm-actions]")).toBeNull();
  });

  it("keeps the one non-wrapping group with Send command among its members", () => {
    const { container } = mount(me(["command:issue", "alarm:acknowledge"]), { ack: true, command: true });
    const group = container.querySelector("[data-alarm-actions]")!;
    expect(group.className).toContain("flex-nowrap");
    expect(group.querySelectorAll("button").length).toBe(2);
  });
});
