import type { Component, JSX } from "solid-js";

// A small lucide-style icon set (stroke-based, currentColor), enough for the
// console shell and the locations views. Each icon takes an optional size.
type P = { size?: number };

const Svg = (props: { size?: number; children: JSX.Element; fill?: boolean }) => (
  <svg
    xmlns="http://www.w3.org/2000/svg"
    width={props.size ?? 18}
    height={props.size ?? 18}
    viewBox="0 0 24 24"
    fill={props.fill ? "currentColor" : "none"}
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    style={{ flex: "none" }}
  >
    {props.children}
  </svg>
);

export const Home: Component<P> = (p) => (
  <Svg size={p.size}><path d="M3 9.5 12 3l9 6.5" /><path d="M5 10v10h14V10" /></Svg>
);
export const LayoutDashboard: Component<P> = (p) => (
  <Svg size={p.size}><rect x="3" y="3" width="7" height="9" rx="1" /><rect x="14" y="3" width="7" height="5" rx="1" /><rect x="14" y="12" width="7" height="9" rx="1" /><rect x="3" y="16" width="7" height="5" rx="1" /></Svg>
);
export const Bell: Component<P> = (p) => (
  <Svg size={p.size}><path d="M6 9a6 6 0 1 1 12 0c0 5 2 6 2 6H4s2-1 2-6" /><path d="M10 20a2 2 0 0 0 4 0" /></Svg>
);
export const Package: Component<P> = (p) => (
  <Svg size={p.size}><path d="m12 3 8 4.5v9L12 21l-8-4.5v-9z" /><path d="M4 7.5 12 12l8-4.5" /><path d="M12 12v9" /></Svg>
);
export const Layers: Component<P> = (p) => (
  <Svg size={p.size}><path d="m12 3 9 5-9 5-9-5z" /><path d="m3 13 9 5 9-5" /></Svg>
);
export const Compass: Component<P> = (p) => (
  <Svg size={p.size}><circle cx="12" cy="12" r="9" /><path d="m15 9-2 6-4 2 2-6z" /></Svg>
);
export const GraduationCap: Component<P> = (p) => (
  <Svg size={p.size}><path d="m12 4 10 5-10 5L2 9z" /><path d="M6 11v5c0 1 3 3 6 3s6-2 6-3v-5" /></Svg>
);
export const Settings: Component<P> = (p) => (
  <Svg size={p.size}><circle cx="12" cy="12" r="3" /><path d="M19 12a7 7 0 0 0-.1-1l2-1.5-2-3.4-2.3 1a7 7 0 0 0-1.7-1l-.3-2.6h-4l-.3 2.6a7 7 0 0 0-1.7 1l-2.3-1-2 3.4 2 1.5a7 7 0 0 0 0 2l-2 1.5 2 3.4 2.3-1a7 7 0 0 0 1.7 1l.3 2.6h4l.3-2.6a7 7 0 0 0 1.7-1l2.3 1 2-3.4-2-1.5a7 7 0 0 0 .1-1z" /></Svg>
);
export const ChevronDown: Component<P> = (p) => (
  <Svg size={p.size}><path d="m6 9 6 6 6-6" /></Svg>
);
export const PanelLeft: Component<P> = (p) => (
  <Svg size={p.size}><rect x="3" y="3" width="18" height="18" rx="2" /><path d="M9 3v18" /></Svg>
);
export const Sun: Component<P> = (p) => (
  <Svg size={p.size}><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M4 12H2M22 12h-2M5 5l1.5 1.5M17.5 17.5 19 19M19 5l-1.5 1.5M6.5 17.5 5 19" /></Svg>
);
export const Moon: Component<P> = (p) => (
  <Svg size={p.size}><path d="M21 12.8A9 9 0 1 1 11.2 3 7 7 0 0 0 21 12.8" /></Svg>
);
export const Search: Component<P> = (p) => (
  <Svg size={p.size}><circle cx="11" cy="11" r="7" /><path d="m21 21-4.3-4.3" /></Svg>
);
export const X: Component<P> = (p) => (
  <Svg size={p.size}><path d="M18 6 6 18M6 6l12 12" /></Svg>
);
export const Plus: Component<P> = (p) => (
  <Svg size={p.size}><path d="M12 5v14M5 12h14" /></Svg>
);
export const ArrowRight: Component<P> = (p) => (
  <Svg size={p.size}><path d="M5 12h14M13 6l6 6-6 6" /></Svg>
);
export const MapPin: Component<P> = (p) => (
  <Svg size={p.size}><path d="M20 10c0 6-8 12-8 12s-8-6-8-12a8 8 0 0 1 16 0" /><circle cx="12" cy="10" r="3" /></Svg>
);
export const Sliders: Component<P> = (p) => (
  <Svg size={p.size}><path d="M4 6h10M18 6h2M4 12h2M10 12h10M4 18h8M16 18h4" /><circle cx="16" cy="6" r="2" /><circle cx="8" cy="12" r="2" /><circle cx="14" cy="18" r="2" /></Svg>
);
export const Pencil: Component<P> = (p) => (
  <Svg size={p.size}><path d="M12 20h9" /><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z" /></Svg>
);
export const Trash: Component<P> = (p) => (
  <Svg size={p.size}><path d="M3 6h18M8 6V4h8v2M6 6l1 14h10l1-14" /></Svg>
);
export const ChevronRight: Component<P> = (p) => (
  <Svg size={p.size}><path d="m9 6 6 6-6 6" /></Svg>
);
export const ChevronLeft: Component<P> = (p) => (
  <Svg size={p.size}><path d="m15 18-6-6 6-6" /></Svg>
);
export const ArrowUpRight: Component<P> = (p) => (
  <Svg size={p.size}><path d="M7 17 17 7" /><path d="M7 7h10v10" /></Svg>
);
export const Maximize: Component<P> = (p) => (
  <Svg size={p.size}><path d="M8 3H5a2 2 0 0 0-2 2v3" /><path d="M21 8V5a2 2 0 0 0-2-2h-3" /><path d="M3 16v3a2 2 0 0 0 2 2h3" /><path d="M16 21h3a2 2 0 0 0 2-2v-3" /></Svg>
);
export const Check: Component<P> = (p) => (
  <Svg size={p.size}><path d="m20 6-11 11-5-5" /></Svg>
);
export const Columns: Component<P> = (p) => (
  <Svg size={p.size}><rect x="3" y="3" width="18" height="18" rx="2" /><path d="M12 3v18" /></Svg>
);
export const Rows: Component<P> = (p) => (
  <Svg size={p.size}><rect x="3" y="3" width="18" height="18" rx="2" /><path d="M3 12h18" /></Svg>
);
export const ListTree: Component<P> = (p) => (
  <Svg size={p.size}><path d="M21 12h-8" /><path d="M21 6H8" /><path d="M21 18h-8" /><path d="M3 6v4a2 2 0 0 0 2 2h3" /><path d="M3 10v6a2 2 0 0 0 2 2h3" /></Svg>
);
export const ChevronsUpDown: Component<P> = (p) => (
  <Svg size={p.size}><path d="m7 15 5 5 5-5" /><path d="m7 9 5-5 5 5" /></Svg>
);
export const ChevronsDownUp: Component<P> = (p) => (
  <Svg size={p.size}><path d="m7 20 5-5 5 5" /><path d="m7 4 5 5 5-5" /></Svg>
);
export const GripVertical: Component<P> = (p) => (
  <Svg size={p.size}><circle cx="9" cy="5" r="1" /><circle cx="9" cy="12" r="1" /><circle cx="9" cy="19" r="1" /><circle cx="15" cy="5" r="1" /><circle cx="15" cy="12" r="1" /><circle cx="15" cy="19" r="1" /></Svg>
);
export const Clock: Component<P> = (p) => (
  <Svg size={p.size}><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></Svg>
);
export const Star: Component<{ size?: number; filled?: boolean }> = (p) => (
  <Svg size={p.size} fill={p.filled}><path d="m12 2 3 6.5 7 .9-5 4.8 1.3 7-6.6-3.6L5 21.2l1.3-7-5-4.8 7-.9z" /></Svg>
);
