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

// sessionShowsReason: the reason is what an operator acts on, so it shows for
// the two states where it says something, down (why the node gave up for now)
// and rejected (what refused it). A rejected session keeps showing its reason
// once stale, since the refusal is what needs fixing however old it is.
export function sessionShowsReason(iface: ReachEndpoint, now: number = Date.now()): boolean {
  const s = iface.session;
  if (!s?.reason) return false;
  if (s.state === "rejected") return true;
  return s.state === "down" && sessionWord(iface, now) === "down";
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
      rungs.push({ key: "authenticated", role: "login", status: observed(iface.authenticated ? iface.authenticated.value === "yes" : undefined) });
    }
  }
  const gate = iface.rejection;
  if (!gate) return rungs;
  return rungs.map((r) => {
    if (r.key === gate.rung) return { ...r, status: "held" };
    if (RANK[r.key] > RANK[gate.rung]) return { ...r, status: "untried" };
    return r;
  });
}

// What each rung is, then what it says now: the tooltip a rung chip carries,
// so the ladder teaches itself one hover at a time.
const RUNG_WHAT: Record<RungRole, string> = {
  informational: "Reachable: a ping, informational only. A component that blocks ping never reads down for it.",
  gate: "Responsive: the transport answering (a connect, or the protocol's handshake). This is the gate: nothing above it is tried until it passes.",
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
    untried: "Not tried while a rung below is held.",
    unknown: "Nothing has measured it yet.",
  },
  authenticated: {
    pass: "The login was accepted.",
    fail: "The login was refused.",
    held: "The login was refused. Nothing logs in again until the credential changes or someone retries the endpoint.",
    untried: "Not tried while the transport rung is held.",
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

// A retry in hand, per endpoint: the session's transition time when it was
// asked for, and when. Kept in the query cache rather than in one notice, so
// every notice for the endpoint (the component's Collection card and the
// endpoint's blade show one each) holds the retry, and neither can stack a
// second redial while the node has not picked up the first.
export type RetryPending = { ts: string; at: number };
export const RETRY_PENDING_KEY = (endpointID: string) => ["session-retry", endpointID] as const;

// RETRY_WAIT_MS bounds how long a retry is held in hand: past it, a node that
// never answered (offline, the endpoint moved) no longer keeps Retry spinning.
export const RETRY_WAIT_MS = 60_000;

// retryWaiting: a retry is in hand while the session has not moved since it was
// asked for (its transition time unchanged) and the wait has not run out.
export function retryWaiting(pending: RetryPending | null | undefined, sessionTS: string | undefined, now: number): boolean {
  return !!pending && pending.ts === (sessionTS ?? "") && now - pending.at < RETRY_WAIT_MS;
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
