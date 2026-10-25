# A tape that passes
#
# Reads one component through the CLI by its dotted address, after a lookup
# and a wait done unseen, and shows a command expected to fail, guarded.

Hide
Type `demo_hush 'lobby=$(omniglass component list | jq -r ".components[] | select(.location == \"lobby\") | .path" | head -n1)'`
Enter
Wait
Type `demo_until 30 'test -n "$lobby"'`
Enter
Wait@40s
Show
Type `omniglass component get "$lobby" | jq -c '{name, product, location}'`
Enter
Wait
Type "test -e /never-there || echo 'nothing there, as expected'"
Enter
Wait
