// Hand-written declarations for shotviewport.mjs (plain ESM so shot.mjs runs it without a build step).
export type Viewport = { width: number; height: number };
export const HOUSE_VIEWPORT: Viewport;
export function parseViewport(text: string | undefined): Viewport | null;
