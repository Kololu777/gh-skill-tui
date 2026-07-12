# The source moved on since install: hello-world shows ↓ and gets updated.
# Prepare: DEMO_ROOT=... ./demo-env.sh state-update
Output update.gif
Set Shell "bash"
Set FontSize 15
Set Width 1200
Set Height 700
Set Padding 16
Set Theme "Catppuccin Mocha"
Set TypingSpeed 60ms

Hide
Type `source ${DEMO_ROOT:?}/env.sh && clear`
Enter
Sleep 1.5s
Show

Type "gh-skill-tui"
Sleep 500ms
Enter
Wait+Screen@30s /hello-world/
Sleep 2.5s

# hello-world is marked ↓ (outdated); move onto it
Type "1"
Sleep 800ms
Type "j"
Sleep 700ms
Type "j"
Sleep 1.2s
Space
Sleep 900ms

# the plan proposes an update instead of a fresh install
Type "i"
Sleep 3.5s
Enter
Wait+Screen@90s /succeeded/
Sleep 5s
