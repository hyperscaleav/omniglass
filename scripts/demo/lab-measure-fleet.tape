# A lab measurement names the fleet the lab really runs
#
# omniglass lab run stamps the scenario it seeded, and a scale lab's size, on every container it
# starts, and omniglass lab measure reads them back to name its report. A measurement of the lab
# already running quotes the fleet it measured, never a flag's default, so a figure compares only
# with one read from the same fleet at the same size: corporate at 20 systems is the measuring
# ladder's smoke size, then 300, 1,000 and 3,000 (#1719).

# demo-needs: docker-socket

Hide
Type `demo_hush 'omniglass lab run --scenario corporate --systems 20 --port 0 >/tmp/scale.log 2>&1 & scale=$!'`
Enter
Wait
Type `demo_until 170 'grep -q "lab is ready" /tmp/scale.log'`
Enter
Wait@180s
Show

Type "# A corporate lab of 20 systems is up, the smoke size: the smallest round size that seeds every product"
Enter
Wait
Type "# Measure the lab already running: no --systems, so lab measure reads the fleet off the lab's own containers"
Enter
Wait
Type "omniglass lab measure --warmup 10s --window 30s --fleet-reads 3 --backlog 0 --json fleet.json >table.txt 2>measure.log"
Enter
Wait@240s
Type "# The table's first line names the scenario and the size the lab was brought up at"
Enter
Wait
Type "grep '^scale lab:' table.txt"
Enter
Wait
Type "# And the JSON a later run compares against carries the same fleet, so only a like run compares"
Enter
Wait
Type `jq -c '{scenario, systems, sites, components}' fleet.json`
Enter
Wait

Hide
Type `demo_hush 'kill -INT "$scale"; wait "$scale"'`
Enter
Wait@90s
Show
