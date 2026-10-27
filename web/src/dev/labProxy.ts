// Where `npm run dev` proxies /api (#1196): a lab running on this machine
// (./bin/omniglass lab run). IPv4 loopback, not localhost, because Node may
// resolve localhost to ::1 while the lab publishes on 127.0.0.1 only.
// OMNIGLASS_LAB_URL moves it, for a lab on another --port.
export const defaultLabURL = "http://127.0.0.1:8080";

export function labProxyTarget(env: Record<string, string | undefined>): string {
  return env.OMNIGLASS_LAB_URL || defaultLabURL;
}
