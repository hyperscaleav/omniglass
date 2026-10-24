import type { components } from "../api/schema.gen";

// A tls endpoint's handshake and certificate as the console reads them
// (#1035): the reachability read carries the mode, the pinned key, the latest
// handshake time, the expiry and the days left; this file only says what each
// means to an operator. Pure.

export type ReachTLS = components["schemas"]["ReachTLSBody"];

export type Tone = "ok" | "warn" | "bad" | "unknown";

// WARN_DAYS is when a certificate's days left turn amber: a month is the
// window an operator renews in.
export const WARN_DAYS = 30;

// daysLeftWord renders the days a certificate has, to the whole day.
export function daysLeftWord(days: number): string {
  if (days < 0) {
    const ago = Math.round(-days);
    if (ago === 0) return "expired";
    return `expired ${ago} day${ago === 1 ? "" : "s"} ago`;
  }
  const left = Math.round(days);
  if (left === 0) return "expires within a day";
  return `${left} day${left === 1 ? "" : "s"} left`;
}

// certTone is the dot a certificate's days left wear: healthy with a month or
// more, amber inside it, red once it has expired.
export function certTone(days: number | undefined | null): Tone {
  if (days === undefined || days === null) return "unknown";
  if (days <= 0) return "bad";
  return days < WARN_DAYS ? "warn" : "ok";
}

const MODES: Record<string, { word: string; tone: Tone; hint: string }> = {
  verify: {
    word: "verified against the node's roots",
    tone: "ok",
    hint: "Verify: the certificate must chain to one of the node's system roots and name the host. Anything else is refused as certificate, and the node stops trying until the endpoint changes.",
  },
  pin: {
    word: "pinned on first use",
    tone: "ok",
    hint: "Pin: the node trusted the first public key the component offered and refuses any other, as an ssh host key is pinned. A renewal that keeps the key is no change; a new key reads rejected until someone retries the endpoint with it.",
  },
  skip: {
    word: "not verified",
    tone: "warn",
    hint: "Skip: nothing about the certificate is checked, so anything answering at this address is trusted. The operator chose it; it is never a driver's default.",
  },
};

// tlsMode names a verification mode, its tone (skip is a warning) and what it
// trusts; an unknown mode reads as itself.
export function tlsMode(mode: string): { word: string; tone: Tone; hint: string } {
  return MODES[mode] ?? { word: mode, tone: "unknown", hint: `The endpoint's tls mode is ${mode}.` };
}

// shortKey trims a SHA256 fingerprint for a dense row; the full key rides the
// row's title.
export function shortKey(key: string): string {
  const m = /^SHA256:(.{12}).+$/.exec(key);
  return m ? `SHA256:${m[1]}…` : key;
}
