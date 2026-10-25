# A tape whose command fails
#
# Reads a component that does not exist: the CLI exits non-zero, so the demo
# fails, naming the command.

Type `omniglass component get 'nowhere.$comp.nothing' | jq -c .`
Enter
Wait
Type "echo the tape carries on, and still fails"
Enter
Wait
