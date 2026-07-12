# A locally edited copy (marked m) is proposed back to the source as a PR.
# Prepare: DEMO_ROOT=... ./demo-env.sh state-pr
# Note: this creates a real branch + PR on the source repository.
Output pr.gif
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

# hello-world is marked m (edited locally); select it
Type "1"
Sleep 800ms
Type "j"
Sleep 700ms
Type "j"
Sleep 1.5s
Space
Sleep 900ms

# p builds a PR plan with the SKILL.md diff; enter ships it
Type "p"
Sleep 4.5s
Enter
Wait+Screen@120s /pull|failed/
Sleep 6s
