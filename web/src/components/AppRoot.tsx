import { type ParentComponent } from "solid-js";
import LabBanner from "./LabBanner";
import { useAuthStatus } from "../lib/authStatus";

// AppRoot is the router's root layout: the one place the lab banner renders
// (#2275), above everything a route draws (the login screen, the signed-in
// shell and its top bar), so it cannot drift between screens. The routed
// screen fills the rest of the viewport below it.
const AppRoot: ParentComponent = (props) => {
  const authStatus = useAuthStatus();
  return (
    <div class="flex min-h-screen flex-col" data-app-root>
      <LabBanner lab={authStatus.data?.lab} />
      <div class="flex min-w-0 flex-1 flex-col">{props.children}</div>
    </div>
  );
};

export default AppRoot;
