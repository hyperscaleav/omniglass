import { Navigate, useLocation } from "@solidjs/router";

// The re-homed index addresses (#798): the bare /locations, /systems, and
// /components URLs land on the fleet list face's matching kind tab. Only the
// index address moved; the :id detail routes still render their pages.
export default function FleetRedirect() {
  const location = useLocation();
  // pathname carries the router base (/web/locations); the kind is the last
  // segment, which is also the tab key.
  const kind = location.pathname.replace(/\/+$/, "").split("/").pop();
  return <Navigate href={`/fleet?view=list&kind=${kind}`} />;
}
