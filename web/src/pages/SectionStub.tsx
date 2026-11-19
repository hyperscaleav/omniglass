import { useLocation } from "@solidjs/router";
import Placeholder from "../components/Placeholder";
import { lookupNav } from "../lib/nav";

// Generic stub for an IA section whose page lands in a later slice; resolves its
// title + hint from the nav tree by the current path.
export default function SectionStub() {
  const loc = useLocation();
  const outpost = () => lookupNav(loc.pathname);
  return <Placeholder title={outpost().label} hint={outpost().hint} issue={outpost().issue} icon={outpost().icon} />;
}
