import { Navigate, useLocation } from "@solidjs/router";

// The re-homed addresses (#798, moved again by #826): the bare /locations,
// /systems, and /components URLs land on Explore's table face on the matching
// kind tab, and the retired /fleet canvas address lands on Explore. Only the
// index addresses moved; the :id routes still render their workspaces.
const KINDS = new Set(["locations", "systems", "components"]);

export default function FleetRedirect() {
  const location = useLocation();
  // pathname carries the router base (/web/locations); the kind is the last
  // segment, which is also the tab key.
  const kind = location.pathname.replace(/\/+$/, "").split("/").pop();
  if (kind === "fleet") {
    // /fleet?view=list&kind=<kind> is the address main sent every bare index
    // visit to (#798), so it sits in bookmarks: it was a table, and lands on
    // the table. Anything else on /fleet was the canvas, and lands on Explore.
    const params = new URLSearchParams(location.search);
    if (params.get("view") === "list") {
      const tab = params.get("kind") ?? "";
      return <Navigate href={`/explore?face=table&kind=${KINDS.has(tab) ? tab : "locations"}`} />;
    }
    return <Navigate href="/explore" />;
  }
  return <Navigate href={`/explore?face=table&kind=${kind}`} />;
}
