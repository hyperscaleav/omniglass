import { type ParentComponent, Show } from "solid-js";
import { useNavigate } from "@solidjs/router";
import LabBanner from "./LabBanner";
import ImpersonationBanner from "./ImpersonationBanner";
import ConfirmHost from "./ConfirmHost";
import { useAuthStatus } from "../lib/authStatus";
import { useMe } from "../lib/auth";

// AppRoot is the router's root layout: the one place the banner strips render
// (the lab strip #2275, the impersonation strip), above everything a route
// draws (the login screen, the signed-in shell and its top bar), so they cannot
// drift between screens. It is exactly one viewport high: the strips take their
// own thin height and the routed screen takes the rest (`min-h-0`), scrolling
// in its own regions, so the document itself never scrolls and a strip never
// leaves the screen (#2433). It also hosts the one destructive confirm
// (#2463), under the router so a blocker's link navigates.
//
// The rule for overlays (drawers, the palette, omnibot): they are `fixed`
// against the viewport and cover the whole window, the strips included, dimmed
// with the shell. The strips are never raised above a drawer or made a place a
// drawer must start below.
const AppRoot: ParentComponent = (props) => {
  const authStatus = useAuthStatus();
  // The impersonation strip needs a session: with nobody signed in there is no
  // one being impersonated, and a stale flag must not show a false strip.
  const me = useMe();
  const navigate = useNavigate();
  return (
    <div class="flex h-dvh flex-col" data-app-root>
      <LabBanner lab={authStatus.data?.lab} />
      <Show when={me.data}>
        <ImpersonationBanner />
      </Show>
      <div class="flex min-h-0 min-w-0 flex-1 flex-col">{props.children}</div>
      <ConfirmHost navigate={(href) => navigate(href)} />
    </div>
  );
};

export default AppRoot;
