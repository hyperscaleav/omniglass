# A lab run fails when performance regresses past its baseline
#
# omniglass lab perf baseline measures throwaway scale labs and writes each gated figure's median
# and spread with the inputs and the host they ran on; omniglass lab perf check compares fresh runs
# (or a sampling run's series) with that baseline through one pure comparison. Exact figures must
# read 0, counts gate on any host, times gate only on the baseline's host, and a figure fails only
# past its tolerance and past the combined spread. Here a baseline from one small lab passes its own
# run, and the same run against a baseline with rows a minute halved fails and names the figure (#1640).

# demo-needs: docker-socket

Hide
Type `demo_hush 'omniglass lab perf baseline --runs 1 --systems 3 --warmup 30s --window 1m --fleet-reads 3 --out /tmp/perf-base.json --append /tmp/perf-runs.jsonl >/tmp/perf-base.txt 2>/tmp/perf-base.err'`
Enter
Wait@480s
Show
Type "# Ran unseen first (about four minutes): omniglass lab perf baseline --runs 1 --systems 3 --window 1m"
Enter
Wait
Type "# It brought up a throwaway corporate lab, measured it, tore it down and wrote the baseline"
Enter
Wait
Type "grep -E '^(perf run|wrote)' /tmp/perf-base.err"
Enter
Wait
Type "# The baseline holds what the runs measured with and the host they ran on"
Enter
Wait
Type `jq -c '{inputs: (.inputs | {scenario, systems, window_seconds}), host: (.host | {cpu, cores}), runs}' /tmp/perf-base.json`
Enter
Wait
Type "# Check the same run against it (--samples reads the run kept with --append): every figure passes"
Enter
Wait
Type "omniglass lab perf check --baseline /tmp/perf-base.json --samples /tmp/perf-runs.jsonl --runs 1 --json /tmp/verdict.json >/tmp/check.txt"
Enter
Wait
Type "grep -E '^(perf gate|host)' /tmp/check.txt"
Enter
Wait
Type `jq -r '.figures | group_by(.status) | map("\(.[0].status): \(length) figures") | .[]' /tmp/verdict.json`
Enter
Wait
Type "# Now a baseline whose metric rows a minute are half what this run counts: a regression"
Enter
Wait
Type `jq '(.figures[] | select(.name == "metric_rows_per_minute_per_system") | .median, .values[]) /= 2' /tmp/perf-base.json > /tmp/halved.json`
Enter
Wait
Type "omniglass lab perf check --baseline /tmp/halved.json --samples /tmp/perf-runs.jsonl --runs 1 --json /tmp/halved.json.verdict >/tmp/halved.txt || echo check exited $?"
Enter
Wait
Type "# The check exits nonzero and names the figure that failed, with its delta and tolerance"
Enter
Wait
Type "grep '^perf gate' /tmp/halved.txt"
Enter
Wait
Type `jq -c '.figures[] | select(.status == "fail") | {name, class, baseline, observed, delta_ratio, tolerance}' /tmp/halved.json.verdict`
Enter
Wait
