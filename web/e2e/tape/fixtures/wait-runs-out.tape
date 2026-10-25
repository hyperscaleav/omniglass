# A tape whose unseen wait runs out
#
# Waits, unseen, for something that never happens: the wait fails, so the demo
# fails, naming it.

Hide
Type `demo_until 3 'test -e /never-there'`
Enter
Wait@20s
Show
Type "echo after the wait"
Enter
Wait
