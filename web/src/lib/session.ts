import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { can, type Me } from "./auth";
import { STALENESS_WINDOW_MS, lastSeenAt, type ReachEndpoint } from "./reachability";

// A held session as the console reads it (#1059): the state the node last
// published for an endpoint's session (ADR-0150, ADR-0155), aged by what the
// endpoint has delivered since, and the reachability ladder the layered gate
// climbs (ADR-0158). Pure: the reachability read carries every value, and
// this file only says what each one means to an operator.

export type ReachSession = components["schemas"]["ReachSessionBody"];
export type ReachRejection = components["schemas"]["ReachRejectionBody"];
export type SessionState = ReachSession["state"];

// stale is not a state the node publishes: it is the read's verdict on a state
// whose endpoint has delivered nothing for the staleness window, so the last
// thing the session said may no longer be true (ADR-0150 point 2).
export type SessionWord = SessionState | "stale";

// sessionWord is the pill's word. A state is stored only on a transition, so
// an up session from last week is fine while the endpoint keeps delivering:
// its age is the endpoint's last_seen, never the transition's own time, which
// stands in only before any stamp (and can then only read older). Null for an
// endpoint that never held a session.
export function sessionWord(iface: ReachEndpoint, now: number = Date.now()): SessionWord | null {
  const s = iface.session;
  if (!s) return null;
  const fresh = lastSeenAt(iface) ?? s.ts;
  if (now - new Date(fresh).getTime() > STALENESS_WINDOW_MS) return "stale";
  return s.state;
}

// ---- the notice (#1059, #1110) ----------------------------------------------

// A Notice is what an endpoint's notice says: a refusal (whatever met it: a
// held session, the reachability probe, a poll) or a down session, why, and
// the time a retry waits to see move (lib/session's retryWaiting).
export type Notice = { kind: "rejected" | "down"; reason: string; waitTS: string };

// What each class refuses, for a gate that carries no reason of its own (one
// published before the node said why): the class is named either way.
const CLASS_REFUSED: Record<string, string> = {
  auth: "the component refused the credential",
  "host-key": "the component presented a host key that is not the one pinned",
  certificate: "the component's certificate was refused",
  protocol: "the component refused the protocol",
};

// retryMark is what a retry waits to see move: the held session's transition
// time where the endpoint holds one (a retry redials it), else the gate's
// publish time (a retry lifts it, and a refusal met again sets it anew).
export function retryMark(iface: ReachEndpoint): string {
  return iface.session?.ts ?? iface.rejection?.ts ?? "";
}

// endpointNotice is the endpoint's notice, or null. A refusal shows whatever
// met it: the gate (endpoint-rejection) holds whether or not a session is held,
// so an endpoint refused by its reachability probe alone reads the same as a
// rejected session (#1110), and a rejected session keeps showing once stale,
// since the refusal is what needs fixing however old it is. Its reason is the
// gate's (the refusal's own words, which a restart never overwrites), else a
// rejected session's (one published before the gate carried a reason), else
// the class. A restarted node starts a session rejected under the gate it
// adopted, and that session's reason is then the node's placeholder, which is
// why the gate's leads. A down session says why the node gave up for now,
// while it is fresh. Everything else is not news.
export function endpointNotice(iface: ReachEndpoint, now: number = Date.now()): Notice | null {
  const s = iface.session;
  const gate = iface.rejection;
  if (gate || s?.state === "rejected") {
    const reason =
      gate?.reason ||
      (s?.state === "rejected" && s.reason) ||
      (gate ? `${gate.class}: ${CLASS_REFUSED[gate.class] ?? "refused in a way retrying cannot fix"}` : "");
    return reason ? { kind: "rejected", reason, waitTS: retryMark(iface) } : null;
  }
  if (s?.state === "down" && s.reason && sessionWord(iface, now) === "down") return { kind: "down", reason: s.reason, waitTS: s.ts };
  return null;
}

// ---- the ladder (ADR-0158) -------------------------------------------------

export type RungKey = "reachable" | "responsive" | "authenticated";
// pass and fail are what the rung last observed; held is a rung a terminal
// refusal gates (nothing at it is attempted again until the connection
// fingerprint moves); untried is a rung above a held one; unknown is a rung
// nothing has measured.
export type RungStatus = "pass" | "fail" | "held" | "untried" | "unknown";
// informational: observed, never gates (the ping). gate: the rung everything
// above waits on (the transport answering). login: the session's auth step.
export type RungRole = "informational" | "gate" | "login";
export type Rung = { key: RungKey; status: RungStatus; role: RungRole };

const RANK: Record<RungKey, number> = { reachable: 0, responsive: 1, authenticated: 2 };

function observed(up: boolean | undefined): RungStatus {
  return up === undefined ? "unknown" : up ? "pass" : "fail";
}

// ladder reads an endpoint's rungs bottom up from the reachability read: the
// informational ping (icmp-reachable), the transport rung (endpoint-responsive
// where a protocol answered, else the tcp connect), and the login
// (endpoint-authenticated), shown only where a login was tried or a gate holds
// it. An icmp endpoint has one rung: its ping is the probe, so it gates. The
// gate (endpoint-rejection) holds its rung and leaves every rung above it
// untried, while the rungs below keep their observations.
export function ladder(iface: ReachEndpoint): Rung[] {
  const ping = iface.layers.find((l) => l.check === "icmp-reachable");
  const port = iface.layers.find((l) => l.check === "tcp-open");
  const reachable: Rung = { key: "reachable", role: iface.transport === "icmp" ? "gate" : "informational", status: observed(ping ? ping.value >= 1 : undefined) };
  const rungs: Rung[] = [reachable];
  if (iface.transport !== "icmp") {
    const transport = iface.responsive ? iface.responsive.value === "up" : port ? port.value >= 1 : undefined;
    rungs.push({ key: "responsive", role: "gate", status: observed(transport) });
    // The login rung exists where a login was tried or a gate holds it:
    // nothing on the read says a driver declares a login it never ran, so a
    // session with no login (a bare line protocol) never grows one.
    if (iface.authenticated || iface.rejection?.rung === "authenticated") {
      // The login is attempted only once the transport answers (ADR-0158):
      // above a failed transport rung it was not tried this time, so an
      // earlier verdict is not shown as current.
      const status: RungStatus = transport === false ? "untried" : observed(iface.authenticated ? iface.authenticated.value === "yes" : undefined);
      rungs.push({ key: "authenticated", role: "login", status });
    }
  }
  const gate = iface.rejection;
  if (!gate) return rungs;
  return rungs.map((r) => {
    if (r.key === gate.rung) return { ...r, status: "held" };
    if (RANK[r.key] > RANK[gate.rung]) return { ...r, status: "untried" };
    // snmp's get carries the community (#821): beneath its auth gate the
    // transport rung below is not sent, so it reads untried, not failed.
    if (iface.transport === "snmp" && gate.rung === "authenticated" && r.key === "responsive") return { ...r, status: "untried" };
    return r;
  });
}

// pollsHeld reports a gate beneath which the node sends no stateless poll
// (#1110): a poll is sent at the transport rung, or for snmp at the session
// rung, since every get carries the community (#821), and a gate at or below
// that rung holds it (the node's statelessRung). A login refused over a
// transport whose polls carry no credential holds none, and neither does a
// gate beside a held session: its polls ride the session, which says for
// itself whether they run.
export function pollsHeld(iface: ReachEndpoint): boolean {
  const rung = iface.rejection?.rung;
  if (!rung || iface.session) return false;
  return RANK[rung] <= RANK[iface.transport === "snmp" ? "authenticated" : "responsive"];
}

// What each rung is, then what it says now: the tooltip a rung chip carries,
// so the ladder teaches itself one hover at a time.
const RUNG_WHAT: Record<RungRole, string> = {
  informational: "Reachable: a ping, informational only. A component that blocks ping never reads down for it.",
  gate: "Responsive: the transport answering (a connect, the protocol's handshake, or, where an open port cannot be detected, a named request answered). This is the gate: nothing above it is tried until it passes.",
  login: "Authenticated: the login, tried only once the transport answers.",
};
const ICMP_WHAT = "Reachable: a ping. For an icmp endpoint the ping is the probe, so it is the gate.";

const RUNG_NOW: Record<RungKey, Record<RungStatus, string>> = {
  reachable: {
    pass: "It answered ping.",
    fail: "It did not answer ping.",
    held: "Held by a refusal.",
    untried: "Not tried while a rung below is held.",
    unknown: "Not pinged.",
  },
  responsive: {
    pass: "The transport answered.",
    fail: "The transport did not answer.",
    held: "Refused in a way retrying cannot fix (a changed host key, a certificate, the protocol). Held until the endpoint changes or someone retries it.",
    untried: "Not tried while a refusal holds the endpoint: a rung below it, or, for snmp, the community the get would carry.",
    unknown: "Nothing has measured it yet.",
  },
  authenticated: {
    pass: "The login was accepted.",
    fail: "The login was refused.",
    held: "The login was refused. Nothing logs in again until the credential changes or someone retries the endpoint.",
    untried: "Not tried while the transport rung is held or does not answer.",
    unknown: "No login has been tried yet.",
  },
};

export function rungHint(r: Rung): string {
  const what = r.key === "reachable" && r.role === "gate" ? ICMP_WHAT : RUNG_WHAT[r.role];
  return `${what} ${RUNG_NOW[r.key][r.status]}`;
}

// ---- retry -----------------------------------------------------------------

// retryRefusal is why the caller may not retry an endpoint, or null when it may.
// A hint for the button only (can() is not scope-aware): the server is the
// authority, and a retry it refuses says why in the response.
export function retryRefusal(me: Me | null | undefined): string | null {
  return can(me, "endpoint", "retry") ? null : "Retrying needs the endpoint:retry permission.";
}

// A retry in hand, per endpoint: its retryMark when it was asked for (the
// session's transition time, or for an endpoint holding none the gate's
// publish time), and when. Kept in the query cache rather than in one notice, so
// every notice for the endpoint (the component's Collection card and the
// endpoint's blade show one each) holds the retry, and neither can stack a
// second redial while the node has not picked up the first.
export type RetryPending = { ts: string; at: number };
export const RETRY_PENDING_KEY = (endpointID: string) => ["session-retry", endpointID] as const;

// RETRY_WAIT_MS bounds how long a retry is held in hand: past it, a node that
// never answered (offline, the endpoint moved) no longer keeps Retry spinning.
export const RETRY_WAIT_MS = 60_000;

// retryWaiting: a retry is in hand while what it waits on has not moved since
// it was asked for (its retryMark unchanged) and the wait has not run out.
export function retryWaiting(pending: RetryPending | null | undefined, mark: string | undefined, now: number): boolean {
  return !!pending && pending.ts === (mark ?? "") && now - pending.at < RETRY_WAIT_MS;
}

// retryEndpoint moves the endpoint's retry counter (POST /endpoints/{id}:retry):
// its connection fingerprint moves, so a gate a refusal holds lifts and the node
// redials once at its next check-in. Audited server-side.
export async function retryEndpoint(id: string): Promise<void> {
  const { error } = await api.POST("/endpoints/{id}:retry", { params: { path: { id } }, body: {} });
  if (error) throw error;
}

// ---- a changed key (#1078) ---------------------------------------------------

// A refusal of a changed key: an ssh host key (class host-key), and next a tls
// certificate's public key (class certificate, #1035). The gate carries both
// keys as data (endpoint-rejection, ADR-0158 as amended by #1078), so the
// console names them, and trusts the exact offered one, without reading a
// fingerprint out of the reason.
export type KeyRefusal = { class: string; pinned: string; offered: string };

// keyRefusal is the endpoint's key refusal, or null: no gate, a gate that is
// not about a key, or half a pair (no evidence).
export function keyRefusal(iface: ReachEndpoint): KeyRefusal | null {
  const r = iface.rejection;
  if (!r?.pinned_key || !r.offered_key) return null;
  return { class: r.class, pinned: r.pinned_key, offered: r.offered_key };
}

const KEY_NOUN: Record<string, string> = { "host-key": "host key", certificate: "certificate key" };

// keyNoun names the key a class refuses, for labels and the confirm's title.
export function keyNoun(cls: string): string {
  return KEY_NOUN[cls] ?? "key";
}

// keyChangedHint teaches the refusal itself, on the notice's eyebrow.
export function keyChangedHint(cls: string): string {
  return `The component presented a ${keyNoun(cls)} that is not the one pinned, so the node refused the connection before logging in or sending anything. It dials no more until someone trusts the offered key or retries the endpoint.`;
}

// What the two keys are, taught on their labels.
export const PINNED_KEY_HINT =
  "The key the node holds the component to: trusted on the first connection, or set by someone who trusted a change. Every connection must present it.";
export const OFFERED_KEY_HINT =
  "The key the component presented instead, on the connection the node refused. Nothing logs in or sends a command past a key that does not match the pin.";

// Why a key changes, taught one hover at a time in the confirm: two causes are
// routine, and the third is the reason the node refuses at all.
export const KEY_CHANGE_CAUSES: { label: string; hint: string }[] = [
  {
    label: "Reinstalled",
    hint: "A firmware reflash, a factory reset or an operating system reinstall mints a new key. Safe to trust when you know the work was done.",
  },
  {
    label: "Hardware swapped",
    hint: "A replacement unit at the same address answers with its own key. Safe to trust when you know the unit was replaced.",
  },
  {
    label: "Man in the middle",
    hint: "Something between the node and the component, answering in its place to read what the node sends (credentials, commands). Never trust a change nobody can account for: compare the offered key with the component's own console first.",
  },
];

// trustRefusal is why the caller may not trust a key, or null when it may.
// The same permission as Retry, since trusting a key is a retry that declares
// a new pin; a hint for the button only, the server is the authority.
export function trustRefusal(me: Me | null | undefined): string | null {
  return can(me, "endpoint", "retry") ? null : "Trusting a key needs the endpoint:retry permission.";
}

// trustOfferedKey declares key as the endpoint's new pin and retries it (POST
// /endpoints/{id}:retry with host_key): the node redials onto it once. key is
// exactly the one the operator was shown; the server refuses (409) any key the
// refusal on record does not name, so a confirm answered against a record that
// moved since cannot pin it. Audited server-side with both keys and the actor.
export async function trustOfferedKey(id: string, key: string): Promise<void> {
  const { error } = await api.POST("/endpoints/{id}:retry", { params: { path: { id } }, body: { host_key: key } });
  if (error) throw error;
}
