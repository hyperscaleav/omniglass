# The anti-pattern catalog

Seeded from the 2026-07-30 audit's failure classes. Each entry is one file in this
directory, named for its id (`<id>.md`), opening with `## <id> (<status>)`: the detection
cue the reviewer sweeps with, the failure it causes, and the fix pattern. Read every file
here; there is no list to keep in step with them.

**Living artifact:** a slice that fixes a class flips the status in that entry's file in
the same PR; a review that finds a new class adds a file. One file per entry is what lets
two PRs add entries without conflicting (#1095), so never collect entries back into one
file (`internal/docslint` fails it). Entries are never silently deleted; a fixed class is
marked `retired` or `fixed` with the fixing PR, because it can regress.
