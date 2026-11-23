import { api } from "../api/client";
import { can, type Me } from "./auth";
import type { Driver } from "./drivers";
import type { MetricRow } from "./metric_types";

// The poll test's data layer (#2427): run one of an endpoint's polls now through
// the outpost that holds it (POST /endpoints/{endpoint}/polls/{poll}:run) and
// read what came back. Pure helpers below keep the component a drawing of them.

export type PollSample = {
  name: string;
  lane?: string;
  value?: number;
  text?: string;
  labels?: Record<string, string>;
  ts: string;
};

export type PollRun = {
  endpoint: string;
  component?: string;
  outpost: string;
  poll: string;
  samples: PollSample[] | null;
  faults?: string[] | null;
  took_ms: number;
};

// runEndpointPoll runs the poll now and lands its samples as a scheduled run's
// do. A refusal is thrown as the problem the server answered (status, detail).
export async function runEndpointPoll(endpoint: string, poll: string): Promise<PollRun> {
  const { data, error } = await api.POST("/endpoints/{endpoint}/polls/{poll}:run", { params: { path: { endpoint, poll }, query: {} } });
  if (error) throw error;
  return data as PollRun;
}

// testPollRefusal is why the caller may not run a poll, or null when it may. A
// hint for the control only (can() is not scope-aware): the server is the authority.
export function testPollRefusal(me: Me | null | undefined): string | null {
  return can(me, "endpoint", "run") ? null : "Testing a poll needs the endpoint:run permission.";
}

// pollNames are the polls a driver's spec declares: the names the run route takes.
export function pollNames(driver: Driver | undefined): string[] {
  return (driver?.spec?.polls ?? []).map((p) => p.name);
}

export type PollFailureKind = "gated" | "no-outpost" | "timeout" | "busy" | "failed";
export type PollFailure = { kind: PollFailureKind; title: string; reason: string };

// classifyPollFailure sorts a refusal into what the operator does next. The
// server words the reason (a gate names its cause and how it reopens), so it is
// carried through as written rather than re-derived.
export function classifyPollFailure(e: unknown): PollFailure {
  const o = e as { status?: number; detail?: string; title?: string } | null | undefined;
  const reason = o?.detail ?? o?.title ?? (e instanceof Error ? e.message : "The poll could not be run.");
  switch (o?.status) {
    case 503:
      return { kind: "no-outpost", title: "The outpost is not connected", reason };
    case 409:
      if (/placed on no outpost/.test(reason)) return { kind: "no-outpost", title: "No outpost collects this endpoint", reason };
      // The title says nothing was sent; the reason keeps the gate's own words.
      if (/^the endpoint is (held|gated), so the poll was not sent: /.test(reason))
        return { kind: "gated", title: "The endpoint is gated, so nothing was sent", reason: reason.replace(/^the endpoint is (held|gated), so the poll was not sent: /, "") };
      return { kind: "failed", title: "The poll was not run", reason };
    case 504:
      return { kind: "timeout", title: "The outpost did not answer in time", reason };
    case 429:
      return { kind: "busy", title: "Too many runs in flight", reason };
    default:
      return { kind: "failed", title: "The poll failed", reason };
  }
}

export type SampleRead = { name: string; text: string; unit?: string; lane?: string; labels?: Record<string, string> };

// readSample is one sample as an operator reads it: a metric's number at its
// type's precision with its unit, a property's text, an event's message.
export function readSample(s: PollSample, metric: MetricRow | undefined): SampleRead {
  const base = { name: s.name, lane: s.lane, labels: s.labels };
  if (s.value !== undefined && s.value !== null) {
    const text = metric?.precision !== undefined && metric.precision !== null ? s.value.toFixed(metric.precision) : String(s.value);
    return { ...base, text, unit: metric?.unit || undefined };
  }
  return { ...base, text: s.text ?? "" };
}
