# Delete managed copies from every agent in one plan.
# Prepare: DEMO_ROOT=... ./demo-env.sh state-delete
Output delete.gif
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

# select the two installed skills (marked ✓)
Type "1"
Sleep 800ms
Type "j"
Sleep 700ms
Space
Sleep 900ms
Type "j"
Sleep 600ms
Space
Sleep 900ms

# build the delete plan and run it
Type "d"
Sleep 3.5s
Enter
Wait+Screen@60s /succeeded/
Sleep 5s
