import { Navigate, useLocation } from "@solidjs/router";

// The re-homed addresses (#798, moved again by #826): the bare /locations,
// /systems, and /components URLs land on Explore's table face on the matching
// kind tab, and the retired /fleet canvas address lands on Explore's tree.
// Only the index addresses moved; the :id detail routes still render their
// pages.
export default function FleetRedirect() {
  const location = useLocation();
  // pathname carries the router base (/web/locations); the kind is the last
  // segment, which is also the tab key.
  const kind = location.pathname.replace(/\/+$/, "").split("/").pop();
  // The retired canvas address (#826) lands on the tree itself.
  if (kind === "fleet") return <Navigate href="/explore" />;
  return <Navigate href={`/explore?face=table&kind=${kind}`} />;
}
