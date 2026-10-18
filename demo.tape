# Recorded on canned data (SWEEP_DEMO), so the demo never shows a real machine.
Output demo.gif

Require go

Set Shell "bash"
Set FontSize 18
Set Width 1280
Set Height 640
Set Padding 24
Set TypingSpeed 60ms
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful
Set WindowBarSize 36
Set BorderRadius 10

Hide
Type `go build -o /tmp/sweep ./cmd/sweep && export SWEEP_DEMO=1 PATH="/tmp:$PATH" && clear && echo "TAPE""_READY"`
Enter
Wait+Screen@300s /TAPE_READY/
Ctrl+L
Show

Type "sweep"
Sleep 400ms
Enter
Sleep 2s

# Pick the whole ~/work/shop directory and stop it.
Type "a"
Sleep 800ms
Type "x"
Sleep 2.2s
Type "y"
Sleep 2.5s

# Select the rest one by one, then stop them too.
Type "j"
Sleep 200ms
Type "j"
Sleep 200ms
Type "j"
Sleep 200ms
Type "a"
Sleep 300ms
Type "j"
Sleep 200ms
Type "j"
Sleep 200ms
Type "a"
Sleep 300ms
Type "j"
Sleep 200ms
Type "j"
Sleep 200ms
Type "a"
Sleep 300ms
Type "j"
Sleep 200ms
Type "j"
Sleep 200ms
Type "j"
Sleep 200ms
Type "a"
Sleep 600ms
Type "x"
Sleep 1.8s
Type "y"
Sleep 2.5s

# Refresh: nothing left.
Type "r"
Sleep 3s
Type "q"
Sleep 500ms
