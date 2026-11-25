---
title: "Advisory findings no longer block the merge (#2222)"
date: 2026-11-25
refs: [2222, 2180]
---

agent-review resolves its own advisory threads right after posting a review (`agent-review.sh advisory-threads`
lists them, the post step resolves them). Advisories stay inline and readable; blocking findings still open
unresolved threads and request changes. Chosen over advisories in the review body (loses the line context) and
over auto-filed issues (one more ticket per review).
