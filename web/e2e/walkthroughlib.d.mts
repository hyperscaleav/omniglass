// Hand-written declarations for walkthroughlib.mjs (plain ESM so the recorder
// runs it without a build step; the unit tests import it through this surface).
export type Target =
  | string
  | { role: string; name?: string; exact?: boolean }
  | { text: string; exact?: boolean }
  | { selector: string };
export type Step = {
  n: number;
  kind: "goto" | "click" | "type" | "select" | "press" | "wait";
  caption: string;
  hold: number;
  poster: boolean;
  path?: string;
  key?: string;
  target?: Target;
  text?: string;
  value?: string;
  timeout?: number;
};
export type Walkthrough = { title: string; description: string; steps: Step[]; poster: number };
export const KINDS: string[];
export const MAX_SECONDS: number;
export const MAX_BYTES: number;
export const DEFAULT_WAIT_MS: number;
export function validateWalkthrough(doc: unknown, file: string): Walkthrough;
export function parseWalkthrough(text: string, file: string): Walkthrough;
export function describeTarget(t: Target): string;
export function describeStep(step: Step): string;
export function stepFailure(id: string, step: Step, timeoutMs: number, reason?: unknown): string;
export function budgetProblems(
  id: string,
  measured: { seconds: number; bytes: number },
  limits?: { maxSeconds?: number; maxBytes?: number },
): string[];
export function stillName(n: number): string;
