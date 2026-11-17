# omniglass platform health reads the platform from the CLI
#
# omniglass platform health reads the platform view through the API with a token alone and reduces it
# to whether Omniglass is keeping up: the system and server verdicts, open alarms, each pipeline hop's
# loss with the end to end, the outposts with loss or not healthy, and every failure figure above zero.
# Its exit status is the verdict. --window adds the growth between two reads, --page-clients times the
# console's system page, and --every samples on an interval, one JSON line a sample (#2055).

Hide
Type `demo_until 150 'omniglass view platform list | jq -e "[.outposts[] | select(.name == \"edge-hq\") | .figures[\"checkin-age\"]] | length == 1"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show
Type "# One command reads whether Omniglass is keeping up; its exit status is the verdict"
Enter
Wait
Type `omniglass platform health; echo "exit $?"`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The same reading as JSON, for a script or a probe: the verdict, and why when it is not healthy"
Enter
Wait
Type `omniglass platform health --json | jq -c '{healthy, reasons, not_visible}' || true`
Enter
Wait
Type "# Each server's verdict and build, and the outposts the reading counts"
Enter
Wait
Type `omniglass platform health --json | jq -c '.servers[] | {name, verdict, version, alarms: (.alarms | length)}' || true`
Enter
Wait
Type `omniglass platform health --json | jq -c '.outposts | {count, with_loss: (.lossy | length), not_healthy: (.not_healthy | length)}' || true`
Enter
Wait

Type "# --window reads twice and adds what grew between: the database and the telemetry stream"
Enter
Wait
Type `omniglass platform health --window 15s --json | jq -c '.window.figures[] | {label, instance, delta, per_minute}' || true`
Enter
Wait
Type "# --page-clients times the console's system page as operators loading it at once feel it"
Enter
Wait
Type `omniglass platform health --window 15s --page-clients 2 --page-loads 3 --json | jq -c '.page | {loads: .system_page_ms.n, failed: .system_page_errors}' || true`
Enter
Wait

Type "# --every samples on an interval, one JSON line a sample appended to a file"
Enter
Wait
Type `omniglass platform health --every 10s --samples 3 --append health.jsonl`
Enter
Wait
Type `jq -c '{seq, healthy, servers: [.servers[].verdict]}' health.jsonl`
Enter
Wait
