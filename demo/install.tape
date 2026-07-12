# Install two skills into multiple agents at once.
# Prepare: DEMO_ROOT=... ./demo-env.sh state-install
Output install.gif
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
Sleep 2s

# focus the skills panel and pick two skills
Type "1"
Sleep 800ms
Type "j"
Sleep 700ms
Type "j"
Sleep 700ms
Space
Sleep 900ms
Type "k"
Sleep 600ms
Space
Sleep 900ms

# build the install plan and run it
Type "i"
Sleep 3.5s
Enter
Wait+Screen@90s /succeeded/
Sleep 5s
