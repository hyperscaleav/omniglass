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
export const Save: Component<P> = (p) => (
  <Svg size={p.size}><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" /><path d="M17 21v-8H7v8" /><path d="M7 3v5h8" /></Svg>
);
export const Trash: Component<P> = (p) => (
  <Svg size={p.size}><path d="M3 6h18M8 6V4h8v2M6 6l1 14h10l1-14" /></Svg>
);
export const Download: Component<P> = (p) => (
  <Svg size={p.size}><path d="M12 3v12" /><path d="m7 12 5 5 5-5" /><path d="M5 21h14" /></Svg>
);
export const MoreHorizontal: Component<P> = (p) => (
  <Svg size={p.size} fill><circle cx="5" cy="12" r="1.6" /><circle cx="12" cy="12" r="1.6" /><circle cx="19" cy="12" r="1.6" /></Svg>
);
export const Ban: Component<P> = (p) => (
  <Svg size={p.size}><circle cx="12" cy="12" r="9" /><path d="m5.6 5.6 12.8 12.8" /></Svg>
);
export const Eye: Component<P> = (p) => (
  <Svg size={p.size}><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" /><circle cx="12" cy="12" r="3" /></Svg>
);
export const Key: Component<P> = (p) => (
  <Svg size={p.size}><circle cx="7.5" cy="15.5" r="4.5" /><path d="M10.7 12.3 20 3" /><path d="m17 6 2 2" /><path d="m14 9 2 2" /></Svg>
);
export const EyeOff: Component<P> = (p) => (
  <Svg size={p.size}><path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c6.5 0 10 8 10 8a13.2 13.2 0 0 1-1.67 2.68" /><path d="M6.61 6.61A13.5 13.5 0 0 0 2 12s3.5 8 10 8a9.1 9.1 0 0 0 5.39-1.61" /><line x1="2" y1="2" x2="22" y2="22" /></Svg>
);
export const Copy: Component<P> = (p) => (
  <Svg size={p.size}><rect x="9" y="9" width="13" height="13" rx="2" /><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" /></Svg>
);
export const RefreshCw: Component<P> = (p) => (
  <Svg size={p.size}><path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" /><path d="M3 3v5h5" /><path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16" /><path d="M21 21v-5h-5" /></Svg>
);
export const Mask: Component<P> = (p) => (
  <Svg size={p.size}><path d="M4 6a2 2 0 0 0-2 2v4a5 5 0 0 0 5 5 8 8 0 0 0 5-2 8 8 0 0 0 5 2 5 5 0 0 0 5-5V8a2 2 0 0 0-2-2h-3a8 8 0 0 0-5 2 8 8 0 0 0-5-2z" /><path d="M6 11c1.5 0 2.5.5 3 2" /><path d="M18 11c-1.5 0-2.5.5-3 2" /></Svg>
);
export const RotateCcw: Component<P> = (p) => (
  <Svg size={p.size}><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" /><path d="M3 3v5h5" /></Svg>
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
export const LogOut: Component<P> = (p) => (
  <Svg size={p.size}><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" /><path d="M16 17l5-5-5-5" /><path d="M21 12H9" /></Svg>
);
export const Info: Component<P> = (p) => (
  <Svg size={p.size}><circle cx="12" cy="12" r="10" /><path d="M12 16v-4" /><path d="M12 8h.01" /></Svg>
);
// Place-type glyphs: the leading icon a location wears in the tree, resolved from
// its location_type's icon key (see resolveIcon below).
export const Landmark: Component<P> = (p) => (
  <Svg size={p.size}><path d="M3 22h18" /><path d="m12 2 9 5H3z" /><path d="M6 10v8M10 10v8M14 10v8M18 10v8" /></Svg>
);
export const Building: Component<P> = (p) => (
  <Svg size={p.size}><rect x="5" y="2" width="14" height="20" rx="1.5" /><path d="M9 22v-4h6v4" /><path d="M9 6h.01M12 6h.01M15 6h.01M9 10h.01M12 10h.01M15 10h.01M9 14h.01M12 14h.01M15 14h.01" /></Svg>
);
export const DoorOpen: Component<P> = (p) => (
  <Svg size={p.size}><path d="M13 4h3a2 2 0 0 1 2 2v14" /><path d="M2 20h3M13 20h9" /><path d="M13 4.5 5 6v14l8 1.5z" /><path d="M10 12v.01" /></Svg>
);

// resolveIcon maps a location_type icon key to its glyph component, falling back
// to MapPin for an unknown or missing key, so the API can add a new type icon
// without a coordinated console release (the tree stays renderable meanwhile).
export const iconByName: Record<string, Component<P>> = {
  landmark: Landmark,
  building: Building,
  layers: Layers,
  "door-open": DoorOpen,
  "map-pin": MapPin,
};
export const resolveIcon = (name: string | undefined | null): Component<P> =>
  (name && iconByName[name]) || MapPin;
