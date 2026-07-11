import { Show, createResource } from "solid-js";
import { type Principal, principalAvatarUrl, principalInitials, principalName } from "../lib/principals";

// UserAvatar renders a principal's profile picture when it has one, else the
// initials placeholder (the same gradient disc used across the console). The image
// is fetched lazily as a data URL and only when has_avatar is set, so a directory
// of pictureless users fires no avatar requests. `size` is the daisyUI width class
// (w-7 in the list, larger in a detail); `textClass` sizes the initials to match.
export default function UserAvatar(props: { principal: Principal; size?: string; textClass?: string }) {
  const size = () => props.size ?? "w-7";
  const textClass = () => props.textClass ?? "text-[10px]";
  const [url] = createResource(
    () => props.principal.human?.has_avatar ?? false,
    (has) => (has ? principalAvatarUrl(props.principal.id) : Promise.resolve(null)),
  );
  return (
    <Show
      when={url()}
      fallback={
        <div class="avatar avatar-placeholder">
          <div class={`${size()} rounded-full bg-linear-to-br from-primary to-info text-primary-content`}>
            <span class={`font-data ${textClass()} font-bold uppercase`}>{principalInitials(props.principal)}</span>
          </div>
        </div>
      }
    >
      <div class="avatar">
        <div class={`${size()} rounded-full`}>
          <img src={url()!} alt={principalName(props.principal)} />
        </div>
      </div>
    </Show>
  );
}
