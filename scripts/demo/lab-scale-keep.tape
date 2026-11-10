# A kept scale lab survives a stop
#
# A scale lab is throwaway unless it is asked to keep its data: omniglass lab run with --keep keeps
# the database and data volumes when it stops, and says the command that brings it back. Run again,
# the seed finds the fleet it laid down last time, checks it is this plan's, re-enrolls each site
# node and keeps it rather than seeding again, which is what lets a soak run for days through
# restarts and upgrades (#1633).

# demo-needs: docker-socket

Type "# A corporate scale lab of 3 systems that keeps its data when it stops"
Enter
Wait
Type "omniglass lab run --scenario corporate --systems 3 --keep --port 0"
Enter
Wait+Screen@240s /Your omniglass lab is ready/
Ctrl+C
Wait@90s
Type "# Stopped, its volumes kept; the stop line names the command that brings this lab back"
Enter
Wait
Type "clear"
Enter
Wait
Type "# Run it again: the seed keeps the fleet from the last run instead of seeding a new one"
Enter
Wait
Type "omniglass lab run --scenario corporate --systems 3 --keep --port 0"
Enter
Wait+Screen@240s /Your omniglass lab is ready/
Ctrl+C
Wait@90s

Hide
Type `demo_hush 'omniglass lab run --scenario corporate --systems 3 --fresh --port 0 >/tmp/fresh.log 2>&1 & fresh=$!'`
Enter
Wait
Type `demo_until 170 'grep -q "lab is ready" /tmp/fresh.log'`
Enter
Wait@180s
Type `demo_hush 'kill -INT "$fresh"; wait "$fresh"'`
Enter
Wait@90s
Show
