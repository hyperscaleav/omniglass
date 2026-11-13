# The perf gate fails a lossy run and names the kind, the hop and the cause
#
# Every delivery row the lab's delivery table prints a loss for is an exact figure in the perf gate,
# delivery_loss/<hop>/<kind> (a sim row its protocol, a push-lane row its lane first), which must
# read 0 with no tolerance, and delivery_unsettled_boots counts the boots no ratio was read from.
# Here a small lab's run passes its own baseline with every delivery row at 0; then the same run,
# with three property items edited in as lost at ingest and one boot as unsettled, fails, and each
# failed row says what was lost, where, and why (#1695).

# demo-needs: docker-socket

Hide
Type `demo_hush 'omniglass lab perf baseline --runs 1 --systems 3 --warmup 30s --window 1m --fleet-reads 3 --out /tmp/perf-base.json --append /tmp/perf-runs.jsonl >/tmp/perf-base.txt 2>/tmp/perf-base.err'`
Enter
Wait@480s
Show
Type "# Ran unseen first (about four minutes): omniglass lab perf baseline --runs 1 --systems 3 --window 1m"
Enter
Wait
Type "# The run's delivery rows: what each kind put into the ingest hop, what came out, and the loss"
Enter
Wait
Type `jq -c '.delivery[] | select(.loss != null) | {lane, hop, kind, protocol, in, out, loss}' /tmp/perf-runs.jsonl`
Enter
Wait
Type "# Check the run against its baseline: every delivery figure is an exact pass at 0"
Enter
Wait
Type "omniglass lab perf check --baseline /tmp/perf-base.json --samples /tmp/perf-runs.jsonl --runs 1 --json /tmp/verdict.json >/tmp/check.txt"
Enter
Wait
Type "grep '^perf gate' /tmp/check.txt"
Enter
Wait
Type `jq -c '.figures[] | select(.name | startswith("delivery")) | {name, class, status, observed}' /tmp/verdict.json`
Enter
Wait
Type "# Now edit the run: three property items lost at ingest (outcome missing), and one boot unsettled"
Enter
Wait
Type `jq -c '.delivery |= map(if .kind == "property" and .hop == "ingest" and .lane == "node" then .out -= 3 | .loss = 3 / .in | .outcomes.missing = 3 else . end) | .delivery_unsettled = [{node: "edge-hq", boot: "1"}]' /tmp/perf-runs.jsonl > /tmp/lossy.jsonl`
Enter
Wait
Type "omniglass lab perf check --baseline /tmp/perf-base.json --samples /tmp/lossy.jsonl --runs 1 --json /tmp/lossy.verdict >/tmp/lossy.txt || echo check exited $?"
Enter
Wait
Type "grep '^perf gate' /tmp/lossy.txt"
Enter
Wait
Type "# Each failed row names what was lost, where and the largest outcome behind it; no tolerance"
Enter
Wait
Type `jq -c '.figures[] | select(.status == "fail") | {name, observed, note}' /tmp/lossy.verdict`
Enter
Wait
