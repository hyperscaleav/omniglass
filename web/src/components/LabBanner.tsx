import { Show } from "solid-js";
import InfoTip from "./InfoTip";

// LabBanner says, on every screen, that this console is an omniglass lab
// (`omniglass lab run`, #1195): its data is disposable and its login is the
// well-known one every walkthrough uses, so it is never to be mistaken for a
// real deployment. The server reports it on the public /auth/status, so the
// login page shows it before anyone signs in. Rendered once, by AppRoot (#2275),
// above the top bar on every route, one thin line (24 px) that never scrolls away (#2433), the long explanation in a tooltip, in the info hue, because it is a fact about
// the instance rather than a warning about an action.
export default function LabBanner(props: { lab: boolean | undefined }) {
  return (
    <Show when={props.lab}>
      <div
        class="flex h-6 flex-none items-center gap-2 border-b border-info/40 bg-info/10 px-4 text-xs leading-none"
        role="status"
        data-testid="lab-banner"
      >
        <span class="badge badge-info badge-xs font-semibold uppercase tracking-wide">Lab instance</span>
        <span class="truncate text-base-content/70">Disposable data, well-known login.</span>
        <InfoTip
          label="the lab instance"
          text="Disposable data and a well-known login, for learning on simulated components. Not a real deployment."
        />
      </div>
    </Show>
  );
}
