import { A, useLocation } from "@solidjs/router";
import { hostPlatform, shortcutHint } from "../lib/platform";
import { Compass, Home, Layers } from "../components/icons";

// The not-found face: its own page, not a placeholder for something to come.
// It names the address that was asked for and offers the doors a lost person
// wants: Home, Explore, and the command palette.
export default function NotFound() {
  const loc = useLocation();
  const asked = () => loc.pathname + loc.search;
  return (
    <div class="flex min-h-[58vh] items-center justify-center">
      <div class="flex max-w-md flex-col items-center gap-3 text-center">
        <span class="text-base-content/20"><Layers size={34} /></span>
        <span class="text-lg font-semibold">Not found</span>
        <p class="text-sm text-base-content/55">
          Nothing in the console lives at <code class="font-data break-all text-base-content/80">{asked()}</code>.
        </p>
        <div class="flex items-center gap-2">
          <A href="/" class="btn btn-sm"><Home size={14} />Home</A>
          <A href="/explore" class="btn btn-sm"><Compass size={14} />Explore</A>
        </div>
        <p class="text-xs text-base-content/45">
          <kbd class="kbd kbd-sm">{shortcutHint(hostPlatform(), "K")}</kbd> to search and jump
        </p>
      </div>
    </div>
  );
}
