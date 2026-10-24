---
title: "The walkthrough stack checks in every two seconds (#934)"
date: 2026-10-20
refs: [934]
---

The walkthrough stack's node now runs with a 2s heartbeat where it ran with 5s (`web/e2e/lib/stack.sh`,
a test-stack setting; the product's `node run --heartbeat` default is unchanged). Each check-in pulls the
worklist and runs the probes, so a walkthrough's waits on new tasks and on a probe's verdict ride on it.
Set up monitoring now records in 48s to 51s (three runs). Watch a rule raise an alarm records in 44s to
53s (three runs), most of it the sustain, the sweep and the blade's poll, which the heartbeat does not
touch, so it still ends on the open alarm: the replug and the clear were recorded on top of it and
measured 64.1s ending on the panel reading healthy again and 68.2s ending on the rule's blade at
**Open alarms (0)**, both over the 60s budget. The script header and the rules guide carry the numbers.
