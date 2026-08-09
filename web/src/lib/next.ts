// The pre-auth deep link, both halves in one place.
//
// AuthGuard captures where an unauthenticated operator was headed and Login
// sends them there after they sign in. Those are inverse operations, and they
// used to live in two files that disagreed: the capture included the query
// string, the resolve returned only the pathname, so an operator bounced off
// /components?system=<uuid> landed on the unscoped list with their filter
// silently gone (#646). Keeping the pair adjacent is what stops that drifting
// again, since a change to one is now visibly a change to the other.
//
// The security property lives here too: a `next` value is only ever accepted
// when it resolves to this origin, which is what makes an open redirect
// impossible regardless of what an attacker puts in the query string.

// BASE is the console's mount point. The router is mounted under it, so a
// stored absolute path carries it and a router-bound path must not.
const BASE = "/web";

// encodeNext turns the location an operator was denied into the value the login
// route carries, or null when the target is the root (nothing worth carrying,
// and a bare `/login` reads better than `/login?next=%2F`).
export function encodeNext(pathname: string, search = "", hash = ""): string | null {
  const target = pathname + search + hash;
  const stripped = target.replace(new RegExp(`^${BASE}`), "") || "/";
  if (stripped === "/") return null;
  return encodeURIComponent(target);
}

// resolveNext turns that value back into a router path: same origin only, the
// base stripped once so the router does not double-count it, and the query and
// fragment carried through, since they are why the operator was going there.
// Anything unparseable, cross-origin, or not rooted resolves to "/" rather than
// throwing, because a failed redirect must still land somewhere sane.
export function resolveNext(raw: unknown, origin: string): string {
  if (typeof raw !== "string" || raw === "") return "/";
  try {
    const u = new URL(decodeURIComponent(raw), origin);
    if (u.origin !== origin) return "/";
    const path = (u.pathname.replace(new RegExp(`^${BASE}`), "") || "/") + u.search + u.hash;
    return path.startsWith("/") ? path : "/";
  } catch {
    return "/";
  }
}
