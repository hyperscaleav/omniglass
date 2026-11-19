---
title: "The old pages drop their badges and leave as their section lands (#2087)"
date: 2026-11-18
refs: [2087, 2153]
---

The docs site is rebuilt by audience under #2153 (ADR-2087.2), so the old pages lose their status
badges now rather than carry ones nobody will maintain. The status grid reads the badge of any page,
wherever it lives, and starts empty: it fills as the rebuilt sections land. The badge-fence lint drops
its `architecture/` filter for the same reason, and a test pins that a badged page in any section is
checked. The old pages stay in place, kept true when a slice falsifies them, until the PR for their
section deletes them; the decision log, the build log, the generated reference and the screenshot and
demo tooling carry over into the new layout.
