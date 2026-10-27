import { Show } from "solid-js";

// LabBanner says, on every screen, that this console is an omniglass lab
// (`omniglass lab run`, #1195): its data is disposable and its login is the
// well-known one every walkthrough uses, so it is never to be mistaken for a
// real deployment. The server reports it on the public /auth/status, so the
// login page shows it before anyone signs in. A sibling of the impersonation
// banner: the same strip under the top bar, in the info hue, because it is a
// fact about the instance rather than a warning about an action.
export default function LabBanner(props: { lab: boolean | undefined }) {
  return (
    <Show when={props.lab}>
      <div
        class="flex items-center gap-3 border-b border-info/40 bg-info/10 px-8 py-2 text-sm"
        role="status"
        data-testid="lab-banner"
      >
        <span class="badge badge-info badge-sm font-semibold uppercase tracking-wide">Lab instance</span>
        <span class="text-base-content/70">
          Disposable data and a well-known login, for learning on simulated components. Not a real deployment.
        </span>
      </div>
    </Show>
  );
}
