import { api } from "../api/client";

// The Command Types catalog data layer: thin typed wrappers over the /command-types
// surface, the "do" twin of the Properties and Event Types catalogs. A command type
// is the driver-owned catalog entry for what a component can be told. settle_window_seconds
// is the driver's actuation-timing fact; the target is a two-armed exclusive arc,
// target_property_type or target_metric_type naming the value a settleable command
// sets (at most one set, both empty for fire-and-forget). Official types are read-only.

export type CommandTypeRow = {
  name: string;
  label?: string;
  description?: string;
  params_schema?: unknown;
  settle_window_seconds: number;
  target_property_type?: string;
  target_metric_type?: string;
  official: boolean;
};

export const COMMAND_TYPES_KEY = ["command_types"] as const;

// A settle window as the operator typed it, read once for both write surfaces.
// `seconds` is what goes on the wire, and its ABSENCE is the point: the field
// folded through `Number(settle()) || 0` before, so unstated, unparseable and
// deliberately-zero all arrived at the server as the same 0 (#718). A window is a
// decision a settleable type asks for, so unstated has to survive as far as the
// body, where it becomes a field that is simply not sent.
export type SettleWindowDraft = {
  // The seconds to send, absent when nothing should be sent.
  seconds?: number;
  // A blocking problem: the surface refuses to submit while this is set.
  error: string | null;
  // A legitimate choice with a consequence worth reading before making it.
  note: string | null;
};

// readSettleWindow judges the typed window against whether the type names a target
// arm. `targeted` is the whole difference: a type naming a target arm settles
// against a reported value, so its window decides when a difference becomes a
// verdict and the operator has to state it; a type with no arm never reaches
// settlement at all, so leaving it blank is the whole answer and the server's own
// default (0) supplies it. Zero is never refused: ADR-0108 makes it a statement of
// intent ("judge it now"), and `reboot` ships that shape.
export function readSettleWindow(typed: string, targeted: boolean): SettleWindowDraft {
  const t = typed.trim();
  if (t === "") {
    return {
      error: targeted
        ? "A settleable command type needs a settle window: the seconds the device is given to actuate, or 0 to judge it at the moment of issue."
        : null,
      note: null,
    };
  }
  // A seconds count, not a number: "1.5" and "1e3" are refused rather than
  // silently truncated, and "abc" is refused rather than read as 0.
  if (!/^-?\d+$/.test(t)) return { error: "A settle window is a whole number of seconds.", note: null };
  const seconds = Number(t);
  if (seconds < 0) return { error: "A settle window is a duration in seconds, so it cannot be negative.", note: null };
  if (targeted && seconds === 0) {
    return {
      seconds,
      error: null,
      note: "0 means this command is judged at the moment it is issued: it settles only if the target already reports what it was told, and fails if it does not.",
    };
  }
  if (!targeted && seconds > 0) {
    return {
      seconds,
      error: null,
      note: "A fire-and-forget command has no value to settle, so this window is never consulted.",
    };
  }
  return { seconds, error: null, note: null };
}

export async function listCommandTypes(): Promise<CommandTypeRow[]> {
  const { data, error } = await api.GET("/command-types");
  if (error) throw error;
  return (data?.command_types ?? []) as CommandTypeRow[];
}

export type CreateCommandType = {
  name: string;
  label?: string;
  description?: string;
  settle_window_seconds?: number;
  target_property_type?: string;
  target_metric_type?: string;
};

export async function createCommandType(body: CreateCommandType): Promise<CommandTypeRow> {
  const { data, error } = await api.POST("/command-types", { body });
  if (error) throw error;
  return data as CommandTypeRow;
}

export type UpdateCommandType = {
  label?: string;
  description?: string;
  settle_window_seconds?: number;
  target_property_type?: string;
  target_metric_type?: string;
};

export async function updateCommandType(name: string, body: UpdateCommandType): Promise<void> {
  const { error } = await api.PATCH("/command-types/{name}", { params: { path: { name } }, body });
  if (error) throw error;
}

export async function deleteCommandType(name: string): Promise<void> {
  const { error } = await api.DELETE("/command-types/{name}", { params: { path: { name } } });
  if (error) throw error;
}
