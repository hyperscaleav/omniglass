// Test-fixture id minting (#474). Every registry row's `id` is a uuid
// post-ADR-0062 and its name lives in `name`; a fixture that puts the
// handle in the id slot re-creates the pre-uuid world and lets a real
// picker/join break ship green (that is exactly how the #432 regressions
// shipped). Mint fixture ids with uuidFor so they are uuid-shaped and stable
// per handle within a test run, and keep the handle in `name`.
const minted = new Map<string, string>();
let counter = 0;

// uuidFor returns a stable uuid-shaped id for a handle: deterministic within
// the process (same handle, same id) so fixtures can cross-reference rows
// without threading variables.
export function uuidFor(handle: string): string {
  const hit = minted.get(handle);
  if (hit) return hit;
  counter += 1;
  const tail = String(counter).padStart(12, "0");
  const id = `00000000-0000-4000-8000-${tail}`;
  minted.set(handle, id);
  return id;
}
