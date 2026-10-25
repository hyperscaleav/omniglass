import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import type { Endpoint } from "./endpoints";
import type { Driver } from "./drivers";
import { LEAF_POLL_MS } from "./component_leaf";

// The commands data layer (#1155): thin typed wrappers over the generated
// client, plus the pure derivations the commands panel renders. A COMMAND is
// what a component was told: the issue records it, a node takes it from its
// queue and actuates it over an endpoint, the component answers, and the
// command settles from that reply or from the next observation of what it set,
// or fails, or times out when its settle window closes on no evidence. Every
// field read here is the API's (status, phase, the evidence and the reason);
// the words and the steps are derived from them, never invented.

export type Command = components["schemas"]["CommandBody"];

export const COMPONENT_COMMANDS_PREFIX = ["component-commands"] as const;
export const componentCommandsKey = (name: string) => [...COMPONENT_COMMANDS_PREFIX, name] as const;
export const commandKey = (name: string, id: number) => [...COMPONENT_COMMANDS_PREFIX, name, "one", id] as const;

// While a command is in flight its row moves within a second (a reply settles
// it inside its node's cycle), so the history polls quickly; at rest it polls
// on the component view's cadence.
export const COMMANDS_IN_FLIGHT_POLL_MS = 1_000;
export const COMMANDS_IDLE_POLL_MS = LEAF_POLL_MS;

export async function componentCommands(name: string): Promise<Command[]> {
  const { data, error } = await api.GET("/components/{name}/commands", { params: { path: { name }, query: { limit: 50 } } });
  if (error) throw error;
  return (data?.commands ?? []) as Command[];
}

export async function getCommand(name: string, id: number): Promise<Command> {
  const { data, error } = await api.GET("/components/{name}/commands/{id}", { params: { path: { name, id } } });
  if (error) throw error;
  return data as Command;
}

export type IssueCommand = { command_type: string; value?: unknown };

export async function issueCommand(name: string, body: IssueCommand): Promise<Command> {
  const { data, error } = await api.POST("/components/{name}/commands:issue", { params: { path: { name } }, body });
  if (error) throw error;
  return data as Command;
}

// A command is in flight until its status is terminal.
export const inFlight = (c: Pick<Command, "status">) => c.status === "issued";

// The poll a command list needs: quick while any of it is in flight.
export function commandsPollMs(cmds: readonly Pick<Command, "status">[] | undefined): number {
  return (cmds ?? []).some(inFlight) ? COMMANDS_IN_FLIGHT_POLL_MS : COMMANDS_IDLE_POLL_MS;
}

export const PHASE_WORDS: Record<Command["phase"], string> = {
  queued: "queued",
  delivered: "delivered",
  "awaiting-reply": "awaiting reply",
  "awaiting-observation": "awaiting observation",
  settled: "settled",
  failed: "failed",
  "timed-out": "timed out",
};

// A phase's pill: in flight reads as work in progress (info), settled as
// healthy, failed as an error, timed out as a warning (no evidence either
// way, which is not the same as a contrary answer).
export function phaseBadge(phase: Command["phase"]): string {
  switch (phase) {
    case "settled":
      return "badge-healthy";
    case "failed":
      return "badge-soft badge-error";
    case "timed-out":
      return "badge-soft badge-warning";
    default:
      return "badge-soft badge-info";
  }
}

// The value a command set, as text: a JSON string by its text, anything else
// as its JSON; empty for a fire-and-forget command.
export function intendedText(c: Pick<Command, "intended">): string {
  const v = c.intended as unknown;
  if (v === undefined || v === null) return "";
  return typeof v === "string" ? v : JSON.stringify(v);
}

export function observedText(c: Pick<Command, "observed">): string {
  const v = c.observed?.value as unknown;
  if (v === undefined || v === null) return "";
  return typeof v === "string" ? v : JSON.stringify(v);
}

// What the phase means for this command, in words: the teaching line its
// pill's tooltip carries.
export function phaseHint(c: Command): string {
  const node = c.node ? `node ${c.node}` : "its node";
  const target = c.target ?? "its target";
  const want = intendedText(c);
  switch (c.phase) {
    case "queued":
      return `Waiting in the queue for ${node} to take it.`;
    case "delivered":
      return `${node[0].toUpperCase()}${node.slice(1)} has it. Its reply decides nothing here: the next observation of ${target} does.`;
    case "awaiting-reply":
      return `${node[0].toUpperCase()}${node.slice(1)} sent it to the component; the component's reply settles it.`;
    case "awaiting-observation":
      return want
        ? `Waiting for an observation of ${target} that reads ${want} before its settle window closes.`
        : `Waiting for an observation of ${target} before its settle window closes.`;
    case "settled":
      return "It took effect: " + (c.reason ?? "its evidence agreed with what it set.");
    case "failed":
      return "It did not take effect: " + (c.reason ?? "its evidence contradicted what it set.");
    case "timed-out":
      return "Its settle window closed with no evidence either way: " + (c.reason ?? "nothing answered.");
  }
}

// One step of a command's arc, as the detail renders it: reached steps carry
// their time, a step the command skips (no endpoint carries it, or it set
// nothing so it settled at its issue) is left out.
export type CommandStep = { key: "issued" | "delivered" | "executed" | "terminal"; label: string; at?: string; reached: boolean };

export function commandSteps(c: Command): CommandStep[] {
  const steps: CommandStep[] = [{ key: "issued", label: "Issued", at: c.issued_at, reached: true }];
  const carried = !!c.endpoint && c.settled_by !== "issue";
  if (carried) {
    steps.push({ key: "delivered", label: c.node ? `Delivered to ${c.node}` : "Delivered", at: c.delivered_at, reached: !!c.delivered_at });
    steps.push({ key: "executed", label: "Answered", at: c.executed_at, reached: !!c.executed_at });
  }
  const terminal = !inFlight(c);
  steps.push({
    key: "terminal",
    label: terminal ? `${PHASE_WORDS[c.phase][0].toUpperCase()}${PHASE_WORDS[c.phase].slice(1)}` : "Settles, fails or times out",
    at: c.settled_at,
    reached: terminal,
  });
  return steps;
}

// offsetWords is how long after `from` a transition came, as its step reads
// it: a tenth of a second under ten seconds (a reply lands in well under one),
// whole seconds under a minute, then minutes and seconds. Every step after the
// issue reads as an offset from it, so the arc says how long each leg took,
// which a clock time to the minute would hide.
export function offsetWords(from: string, to: string): string {
  const s = Math.max(0, (new Date(to).getTime() - new Date(from).getTime()) / 1000);
  if (s < 10) return `+${s.toFixed(1)}s`;
  if (s < 60) return `+${Math.round(s)}s`;
  const m = Math.floor(s / 60);
  return `+${m}m ${Math.round(s - m * 60)}s`;
}

// The settle window, in words, for an in-flight command: how long it has left
// to settle, or that it has closed and the next sweep will judge it.
export function windowWords(c: Pick<Command, "deadline" | "status">, now: number = Date.now()): string {
  if (!inFlight(c)) return "";
  const left = Math.round((new Date(c.deadline).getTime() - now) / 1000);
  return left > 0 ? `window closes in ${left}s` : "window closed, judging";
}

// The command types an operator can issue to a component: the ones a driver
// attached to one of its endpoints binds, since only those reach the
// component. Sorted, each once.
export function boundCommandTypes(componentId: string, endpoints: readonly Endpoint[], drivers: readonly Driver[]): string[] {
  const byName = new Map(drivers.map((d) => [d.name, d]));
  const out = new Set<string>();
  for (const e of endpoints) {
    if (e.component_id !== componentId || !e.driver) continue;
    for (const b of byName.get(e.driver)?.spec?.commands ?? []) out.add(b.command_type);
  }
  return [...out].sort();
}

// readCommandValue is the rule for the value an issue carries, by what the
// command type sets: a metric takes a number, a property any value (JSON when
// it parses as a JSON scalar, such as true or 42, else the text), and a
// fire-and-forget command none.
export type CommandValueDraft = { value?: unknown; error?: string };

export function readCommandValue(typed: string, targetKind: "property" | "metric" | undefined): CommandValueDraft {
  const t = typed.trim();
  if (!targetKind) return {};
  if (t === "") return { error: targetKind === "metric" ? "Enter the number to set." : "Enter the value to set." };
  if (targetKind === "metric") {
    const n = Number(t);
    return Number.isFinite(n) ? { value: n } : { error: "A metric takes a number." };
  }
  try {
    const v = JSON.parse(t) as unknown;
    if (v === null || typeof v !== "object") return { value: v };
  } catch {
    // not JSON: the text is the value
  }
  return { value: t };
}
