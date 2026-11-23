import { useNavigate } from "@solidjs/router";

// The re-homed addresses (#798, moved by #826 and #861): the bare /locations,
// /systems and /components URLs, the retired /fleet canvas and its list
// address all land on the outline, which holds every location, system and
// component. Only the index addresses moved; the :id routes still render
// their workspaces. Router 2 removed <Navigate>; under base "/web" a path
// starting with "/" is a full path, so the base is spelled out (#2290 spike).
export default function FleetRedirect() {
  useNavigate()("/web/explore", { replace: true });
  return null;
}
