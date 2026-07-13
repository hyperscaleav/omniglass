// The omniglass brand lockup. BrandMark is the real og-icon (a triad of ringed
// nodes around a central hub, with the inner edges masked out where they meet a
// node), themed off --color-primary so it tracks the light/dark toggle. The
// wordmark is all caps. Shared by the sidebar and the login screen.

export function BrandMark(props: { size?: number }) {
  const s = () => props.size ?? 26;
  return (
    <svg width={s()} height={s()} viewBox="-3 -3 166 166" class="flex-none" aria-hidden="true">
      <defs>
        <mask id="og-mark">
          <rect x="0" y="0" width="160" height="160" fill="white" />
          <g fill="black">
            <circle cx="80" cy="22" r="7" />
            <circle cx="24" cy="128" r="7" />
            <circle cx="136" cy="128" r="7" />
            <circle cx="80" cy="93" r="13" />
          </g>
        </mask>
      </defs>
      <g mask="url(#og-mark)" stroke="var(--color-primary)" stroke-linecap="round" fill="none">
        <line x1="80" y1="22" x2="24" y2="128" stroke-width="7" />
        <line x1="80" y1="22" x2="136" y2="128" stroke-width="7" />
        <line x1="24" y1="128" x2="136" y2="128" stroke-width="7" />
        <line x1="80" y1="93" x2="80" y2="22" stroke-width="4" />
        <line x1="80" y1="93" x2="24" y2="128" stroke-width="4" />
        <line x1="80" y1="93" x2="136" y2="128" stroke-width="4" />
      </g>
      <g stroke="var(--color-primary)" fill="none" stroke-width="4">
        <circle cx="80" cy="22" r="9" />
        <circle cx="24" cy="128" r="9" />
        <circle cx="136" cy="128" r="9" />
        <circle cx="80" cy="93" r="15" />
      </g>
      <circle cx="80" cy="93" r="9" fill="var(--color-primary)" />
    </svg>
  );
}

export function Wordmark(props: { class?: string }) {
  return (
    <span class={`og-wordmark font-data font-bold tracking-tight ${props.class ?? ""}`}>
      OMNI<span class="og-glass">GLASS</span>
    </span>
  );
}
