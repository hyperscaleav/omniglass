# A tape whose command fails inside a list
#
# A command that fails before an && is a failure the shell's ERR trap never
# sees; the prompt's own check catches it, so the demo fails, naming the line.

Type "test -e /never-there && echo unreachable"
Enter
Wait
