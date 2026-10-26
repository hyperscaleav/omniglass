---
title: "A capacity refusal names only systems the caller can read (#1216)"
date: 2026-10-26
refs: [1216, 504]
---

Hardening on the standard role write path. Lowering a standard role's capacity is checked against
every conforming system, and the refusal used to name the worst-exceeding one whatever the caller's
scope. It now names the worst-exceeding system the caller may read, and when the caller reads none of
them it says only that a system it cannot read is in the way, with no name or count. The check stays fleet-wide, so nothing a
narrower caller could not see is allowed through; only the refusal's wording follows the caller's
read scope. It reuses the redaction #504 gave the role-name collision on the same route, now one
helper for both refusals.
