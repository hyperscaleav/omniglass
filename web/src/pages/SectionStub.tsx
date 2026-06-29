import { useLocation } from "@solidjs/router";
import Placeholder from "../components/Placeholder";
import { lookupNav } from "../lib/nav";

// Generic stub for an IA section whose page lands in a later slice; resolves its
// title + hint from the nav tree by the current path.
export default function SectionStub() {
  const loc = useLocation();
  const node = () => lookupNav(loc.pathname);
  return <Placeholder title={node().label} hint={node().hint} issue={node().issue} />;
}
