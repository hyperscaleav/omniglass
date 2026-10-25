# No help text ever shows a credential
#
# The CLI's bearer token sits in OMNIGLASS_TOKEN, as a script exports it. Help on
# the root, a generated command, a hand-written one and a public one names
# $OMNIGLASS_TOKEN as the source and never prints the value, shell completion
# describes the flag the same way, and the token still authenticates, read when
# the command runs (#979).

Type "# A bearer token in the environment, as a script exports it"
Enter
Wait
Type `test -n "$OMNIGLASS_TOKEN" && echo "OMNIGLASS_TOKEN holds a ${#OMNIGLASS_TOKEN}-character token"`
Enter
Wait
Sleep 1s

Type "# Help names where the token comes from, never the value"
Enter
Wait
Type `omniglass component list --help | grep -A1 -- --server`
Enter
Wait
Sleep 2s

Type "# Search every page of help below for the token itself"
Enter
Wait
Type `for c in "" "component list" "secret reveal" "node claim" "bootstrap"; do omniglass $c --help; done | wc -l`
Enter
Wait
Type `! (for c in "" "component list" "secret reveal" "node claim" "bootstrap"; do omniglass $c --help; done | grep -qF "$OMNIGLASS_TOKEN") && echo "the token appears on none of them"`
Enter
Wait
Sleep 2s

Type "# Shell completion describes the flag the same way"
Enter
Wait
Type `omniglass __complete component list -- | grep -- --token`
Enter
Wait
Sleep 1s

Type "# And the token still authenticates, read when the command runs"
Enter
Wait
Type `omniglass auth me | jq -c '{username}'`
Enter
Wait
Sleep 3s
