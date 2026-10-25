import { describe, it, expect } from "vitest";
import {
  COMMANDS_IDLE_POLL_MS,
  COMMANDS_IN_FLIGHT_POLL_MS,
  boundCommandTypes,
  commandSteps,
  commandsPollMs,
  intendedText,
  observedText,
  offsetWords,
  phaseBadge,
  phaseHint,
  readCommandValue,
  windowWords,
  type Command,
} from "./commands";
import type { Endpoint } from "./endpoints";
import { uuidFor } from "./testids";
import type { Driver } from "./drivers";

// A command as the API answers it, issued and still queued.
function command(over: Partial<Command> = {}): Command {
  return {
    id: 7,
    component: "videobar-1",
    component_id: "c-1",
    command_type: "set-volume",
    intended: 42 as unknown as Command["intended"],
    target_kind: "metric",
    target: "volume",
    status: "issued",
    phase: "queued",
    settle_mode: "reply",
    settle_window_seconds: 15,
    deadline: "2026-10-25T10:00:15Z",
    issued_at: "2026-10-25T10:00:00Z",
    endpoint: "wss",
    endpoint_id: "e-1",
    node: "edge-hq",
    driver: "kestrel-ws",
    issuer: "dev",
    issuer_id: "p-1",
    ...over,
  } as Command;
}

describe("commandsPollMs", () => {
  it("polls quickly while anything is in flight, at rest otherwise", () => {
    expect(commandsPollMs([command(), command({ status: "settled", phase: "settled" })])).toBe(COMMANDS_IN_FLIGHT_POLL_MS);
    expect(commandsPollMs([command({ status: "failed", phase: "failed" })])).toBe(COMMANDS_IDLE_POLL_MS);
    expect(commandsPollMs(undefined)).toBe(COMMANDS_IDLE_POLL_MS);
  });
});

describe("phaseBadge", () => {
  it("reads in flight as work, settled as healthy, failed as error, timed out as warning", () => {
    expect(phaseBadge("queued")).toContain("badge-info");
    expect(phaseBadge("awaiting-observation")).toContain("badge-info");
    expect(phaseBadge("settled")).toBe("badge-healthy");
    expect(phaseBadge("failed")).toContain("badge-error");
    expect(phaseBadge("timed-out")).toContain("badge-warning");
  });
});

describe("intendedText and observedText", () => {
  it("renders a string by its text and anything else as JSON", () => {
    expect(intendedText(command())).toBe("42");
    expect(intendedText(command({ intended: "hdmi3" as unknown as Command["intended"] }))).toBe("hdmi3");
    expect(intendedText(command({ intended: undefined }))).toBe("");
    expect(observedText(command({ observed: { value: "hdmi3" as unknown as Command["intended"], at: "2026-10-25T10:00:02Z" } }))).toBe("hdmi3");
    expect(observedText(command())).toBe("");
  });
});

describe("phaseHint", () => {
  it("says where an in-flight command waits, naming its node and target", () => {
    expect(phaseHint(command())).toMatch(/queue for node edge-hq/);
    expect(phaseHint(command({ phase: "awaiting-reply" }))).toMatch(/reply settles it/);
    expect(phaseHint(command({ phase: "delivered", settle_mode: "observation" }))).toMatch(/next observation of volume/);
    expect(phaseHint(command({ phase: "awaiting-observation" }))).toMatch(/reads 42/);
  });
  it("carries the reason once terminal", () => {
    expect(phaseHint(command({ status: "timed-out", phase: "timed-out", reason: "queued for node edge-dark" }))).toMatch(/edge-dark/);
  });
});

describe("commandSteps", () => {
  it("walks issued, delivered, answered and the outcome, stamping what is reached", () => {
    const steps = commandSteps(command({ phase: "awaiting-reply", delivered_at: "2026-10-25T10:00:01Z" }));
    expect(steps.map((s) => s.key)).toEqual(["issued", "delivered", "executed", "terminal"]);
    expect(steps.map((s) => s.reached)).toEqual([true, true, false, false]);
    expect(steps[1].label).toBe("Delivered to edge-hq");
    expect(steps[3].label).toMatch(/settles, fails or times out/i);
  });
  it("names the outcome once terminal", () => {
    const steps = commandSteps(command({ status: "timed-out", phase: "timed-out", settled_at: "2026-10-25T10:00:20Z" }));
    expect(steps[3]).toMatchObject({ label: "Timed out", reached: true, at: "2026-10-25T10:00:20Z" });
  });
  it("skips the wire for a command no endpoint carries, or one that settled at its issue", () => {
    expect(commandSteps(command({ endpoint: undefined })).map((s) => s.key)).toEqual(["issued", "terminal"]);
    expect(commandSteps(command({ status: "settled", phase: "settled", settled_by: "issue" })).map((s) => s.key)).toEqual(["issued", "terminal"]);
  });
});

describe("offsetWords", () => {
  it("reads each leg as an offset from the issue, finer while it is short", () => {
    const t0 = "2026-10-25T10:00:00.000Z";
    expect(offsetWords(t0, "2026-10-25T10:00:00.420Z")).toBe("+0.4s");
    expect(offsetWords(t0, "2026-10-25T10:00:15.200Z")).toBe("+15s");
    expect(offsetWords(t0, "2026-10-25T10:02:05.000Z")).toBe("+2m 5s");
    expect(offsetWords(t0, "2026-10-25T09:59:59.000Z")).toBe("+0.0s");
  });
});

describe("windowWords", () => {
  it("counts down an in-flight window, and says when it has closed", () => {
    const at = new Date("2026-10-25T10:00:05Z").getTime();
    expect(windowWords(command(), at)).toBe("window closes in 10s");
    expect(windowWords(command(), at + 20_000)).toBe("window closed, judging");
    expect(windowWords(command({ status: "settled" }), at)).toBe("");
  });
});

describe("boundCommandTypes", () => {
  it("offers the types its endpoints' drivers bind, once each, sorted", () => {
    const endpoints = [
      { id: uuidFor("cmd-e1"), name: "wss", transport: "wss", component_id: "c-1", driver: "kestrel-ws" },
      { id: uuidFor("cmd-e2"), name: "https", transport: "https", component_id: "c-1", driver: "kestrel-api" },
      { id: uuidFor("cmd-e3"), name: "icmp", transport: "icmp", component_id: "c-1" },
      { id: uuidFor("cmd-e4"), name: "https", transport: "https", component_id: "c-2", driver: "boreal-edge-web" },
    ] as Endpoint[];
    const drivers = [
      { id: uuidFor("cmd-d1"), name: "kestrel-ws", label: "", official: true, spec: { version: 1, transport: "wss", commands: [{ command_type: "set-volume", request: {} }, { command_type: "set-mute", request: {} }] } },
      { id: uuidFor("cmd-d2"), name: "kestrel-api", label: "", official: true, spec: { version: 1, transport: "https", commands: [{ command_type: "set-volume", request: {} }, { command_type: "set-power", request: {} }] } },
      { id: uuidFor("cmd-d3"), name: "boreal-edge-web", label: "", official: true, spec: { version: 1, transport: "http", commands: [{ command_type: "set-input", request: {} }] } },
    ] as Driver[];
    expect(boundCommandTypes("c-1", endpoints, drivers)).toEqual(["set-mute", "set-power", "set-volume"]);
    expect(boundCommandTypes("c-9", endpoints, drivers)).toEqual([]);
  });
});

describe("readCommandValue", () => {
  it("takes a number for a metric, refusing anything else", () => {
    expect(readCommandValue("42", "metric")).toEqual({ value: 42 });
    expect(readCommandValue(" 4.5 ", "metric")).toEqual({ value: 4.5 });
    expect(readCommandValue("loud", "metric").error).toMatch(/number/);
    expect(readCommandValue("", "metric").error).toMatch(/number to set/);
  });
  it("takes a JSON scalar or the text for a property", () => {
    expect(readCommandValue("hdmi3", "property")).toEqual({ value: "hdmi3" });
    expect(readCommandValue("true", "property")).toEqual({ value: true });
    expect(readCommandValue('{"a":1}', "property")).toEqual({ value: '{"a":1}' });
    expect(readCommandValue("  ", "property").error).toMatch(/value to set/);
  });
  it("takes nothing for a fire-and-forget command", () => {
    expect(readCommandValue("ignored", undefined)).toEqual({});
  });
});
