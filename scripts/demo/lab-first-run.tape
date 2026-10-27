# One command brings up a working omniglass to learn on
#
# On a machine with only Docker, omniglass lab run brings up a working omniglass as one Docker
# project: Postgres, the server, and the edge node edge-hq running the simulator in its own process.
# It says what it is doing while it comes up, waits until the node collects from every simulated
# component, then prints the console's address, the login, how to stop it and where to go next.
# Ctrl-C removes the containers and the network and keeps the data for the next run (#1193).

# demo-needs: docker-socket

Type "# A working omniglass, its database and a node collecting from a simulated fleet: one command"
Enter
Wait
Type "omniglass lab run"
Enter
Wait+Screen@150s /Your omniglass lab is ready/
Sleep 4s
Ctrl+C
Wait@60s
