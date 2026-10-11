import { Navigate } from "@solidjs/router";

// The re-homed addresses (#798, moved by #826 and #861): the bare /locations,
// /systems and /components URLs, the retired /fleet canvas and its list
// address all land on the outline, which holds every place, system and
// component. Only the index addresses moved; the :id routes still render
// their workspaces.
export default function FleetRedirect() {
  return <Navigate href="/explore" />;
}
