# A scale lab shows a refusal on purpose, and its delivery table reads it as loss
#
# omniglass lab run --induce-unregistered brings a scale lab up with one simulated device answering
# a property key no driver registers: the seed registers lab-induced-unregistered, writes a copy of
# that device's driver whose extra poll emits it, attaches the copy, and unregisters the key. Ingest
# refuses every sample of it, and lab measure's delivery table reads a property loss above 0 with
# cause refused-unregistered while every metric row reads 0 (#1698, ADR-1698).

# demo-needs: docker-socket

Hide
Type `demo_hush 'omniglass lab run --scenario corporate --systems 3 --port 0 --induce-unregistered >/tmp/scale.log 2>&1 & scale=$!'`
Enter
Wait
Type `demo_until 170 'grep -q "lab is ready" /tmp/scale.log'`
Enter
Wait@180s
Show

Type "# Brought up unseen: omniglass lab run --scenario corporate --systems 3 --induce-unregistered"
Enter
Wait
Type "# The seed names the device that answers the unregistered key, and the driver copy it runs"
Enter
Wait
Type "grep -o 'scenario corporate.*' /tmp/scale.log | tr ';' '\n'"
Enter
Wait

Hide
Type `demo_hush 'omniglass lab measure --warmup 30s --window 1m --fleet-reads 3 --backlog 0 --json /tmp/induced.json >/tmp/induced.txt 2>/tmp/induced.err'`
Enter
Wait@360s
Type "clear"
Enter
Wait
Show
Type "# Measured unseen over one minute: omniglass lab measure --warmup 30s --window 1m --backlog 0"
Enter
Wait
Type "# The delivery table: property samples lost at ingest, refused-unregistered, while metrics lose nothing"
Enter
Wait
Type "sed -n '/^delivery /,$p' /tmp/induced.txt"
Enter
Wait
Type "# The same rows in the JSON: what became of each property sample, by outcome"
Enter
Wait
Type `jq -c '.delivery[] | select(.kind == "metric" or .kind == "property") | {kind, hop, in, out, loss, outcomes}' /tmp/induced.json`
Enter
Wait

Hide
Type `demo_hush 'kill -INT "$scale"; wait "$scale"'`
Enter
Wait@90s
Show
