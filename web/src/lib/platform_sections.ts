import {
  gapName,
  gapTone,
  hopFigure,
  lossPercent,
  outpostPill,
  panelFlagged,
  ratioText,
  sortGaps,
  type PlatformPipeline,
  type PlatformServer,
  type PlatformSimDevice,
  type PlatformSimOutpost,
  type PlatformView,
} from "./platformhealth";

// The Platform health page's closed sections (#2446). Each section folds to one
// line, a verdict and the one number that matters, so the first screen answers
// "is the platform healthy, and if not, where" with nothing opened. Every line
// is derived from the view the page already holds: the page invents no figure
// and no threshold, it only chooses which one to say.

export type SummaryTone = "ok" | "warn" | "bad" | "none";
export interface Summary {
  tone: SummaryTone;
  text: string;
}

const NONE = (text: string): Summary => ({ tone: "none", text });
const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

// pipelineSummary is the end to end loss over the last hour, and where it is
// worst when there is any: a loss ratio, 0 healthy, never a fake 0 for no data.
export function pipelineSummary(p: PlatformPipeline | null | undefined): Summary {
  const end = p?.end_to_end_total.last_hour;
  if (!p || end === undefined) return NONE("no figures yet");
  if (end === 0) return { tone: "ok", text: "0% lost end to end" };
  let worst: { label: string; loss: number } | undefined;
  for (const h of p.hops ?? []) {
    const loss = hopFigure(h, "all")?.loss;
    if (loss !== undefined && loss > 0 && (!worst || loss > worst.loss)) worst = { label: h.label, loss };
  }
  const where = worst ? `, worst at ${worst.label} (${lossPercent(worst.loss)})` : "";
  return { tone: "bad", text: `${lossPercent(end)} lost end to end${where}` };
}

// serverSummary is the server's open alarms and the panels they flag. A panel
// is flagged only by an alarm the view carries on one of its keys.
export function serverSummary(s: PlatformServer): Summary {
  const alarms = s.alarms ?? [];
  if (alarms.length === 0) return { tone: "ok", text: "no open alarms" };
  const flagged = (s.groups ?? []).filter((g) => panelFlagged(s, g)).map((g) => g.label);
  const names = flagged.length > 1 ? `${flagged.slice(0, -1).join(", ")} and ${flagged[flagged.length - 1]}` : flagged[0];
  return { tone: "warn", text: `${plural(alarms.length, "open alarm")}${names ? `, ${names} flagged` : ""}` };
}

// outpostsSummary is how many outposts are up, by the Outposts page's own rule
// judged at the view's read_at.
export function outpostsSummary(v: PlatformView): Summary {
  const all = v.outposts ?? [];
  if (all.length === 0) return NONE("no outposts yet");
  const up = all.filter((n) => outpostPill(n, v.read_at) === "up").length;
  if (up === all.length) return { tone: "ok", text: `${up} of ${all.length} up` };
  return { tone: "bad", text: `${up} of ${all.length} up, ${all.length - up} not up` };
}

// simulationSummary reads the closed loop: the worst gap nothing explains, or
// no loss, with the devices the simulator serves. An impaired device loses on
// purpose, so its gap is expected and never a fault here.
export function simulationSummary(outposts: PlatformSimOutpost[] | null | undefined): Summary {
  if (!outposts) return NONE("reading");
  if (outposts.length === 0) return NONE("no simulator checking in yet");
  const measured = outposts.filter((n) => n.state === "measured");
  if (measured.length === 0) return NONE(outposts.some((n) => n.state === "warming up") ? "warming up" : "window restarted");
  const devices = measured.reduce((n, o) => n + (o.devices ?? []).length, 0);
  const gaps = measured.flatMap((o) => sortGaps(o.gaps));
  const fault = sortGaps(gaps).find((g) => gapTone(g) === "fault");
  if (fault) return { tone: "bad", text: `losing at ${gapName(fault.gap)} (${ratioText(fault.ratio)})` };
  const expected = gaps.some((g) => gapTone(g) === "expected");
  return { tone: "ok", text: `${expected ? "no loss beyond impairments" : "no loss"}, ${plural(devices, "device")}` };
}

// Open state lives in the URL (?open=pipeline,outposts) so a reload, a back
// step and a shared link keep the sections the operator was reading. No
// parameter is every section closed.
export const OPEN_PARAM = "open";

function openList(raw: string | string[] | undefined): string[] {
  const s = Array.isArray(raw) ? raw.join(",") : (raw ?? "");
  return s.split(",").filter(Boolean);
}

export function isSectionOpen(raw: string | string[] | undefined, id: string): boolean {
  return openList(raw).includes(id);
}

// setSectionOpen is the parameter after opening or closing one section: the
// others keep their place, and the last one closed drops the parameter.
export function setSectionOpen(raw: string | string[] | undefined, id: string, open: boolean): string | undefined {
  const cur = openList(raw);
  const next = open ? (cur.includes(id) ? cur : [...cur, id]) : cur.filter((x) => x !== id);
  return next.length > 0 ? next.join(",") : undefined;
}

// ---- The simulator's device table ----

// devicePlaceText tells repeated device names apart (#1724): the system the
// device is in, then the places from the root down to it.
export function devicePlaceText(d: Pick<PlatformSimDevice, "system" | "place">): string {
  const place = (d.place ?? []).join(" / ");
  return [d.system, place].filter(Boolean).join(" · ");
}

// deviceStateText is every device's state, so the column is never blank: it is
// answering unless the simulator impairs it or it was due and never seen.
export function deviceStateText(d: Pick<PlatformSimDevice, "impaired" | "unhit">): string[] {
  const out = [d.unhit ? "due, never seen" : "", d.impaired ? "impaired" : ""].filter(Boolean);
  return out.length > 0 ? out : ["answering"];
}

// filterDevices keeps the devices whose name, system or place holds every word
// of the query, in any case.
export function filterDevices(devices: PlatformSimDevice[], query: string): PlatformSimDevice[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return devices;
  return devices.filter((d) => {
    const hay = `${d.component} ${d.system} ${(d.place ?? []).join(" ")}`.toLowerCase();
    return words.every((w) => hay.includes(w));
  });
}

export function pageCount(total: number, size: number): number {
  return Math.max(1, Math.ceil(total / size));
}

// pageSlice is one page of a list; a page past either end clamps to it.
export function pageSlice<T>(items: T[], page: number, size: number): T[] {
  const p = Math.min(Math.max(page, 0), pageCount(items.length, size) - 1);
  return items.slice(p * size, p * size + size);
}
