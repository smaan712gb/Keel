# Keel two-agent contention demo — renders a <8s GIF for the README.
#
# Prerequisites on the recording host:
#   - vhs    (https://github.com/charmbracelet/vhs)
#   - ffmpeg (vhs dependency)
#   - jq     (used to capture the lock id from --json output)
#   - keel on PATH; `keel init` has been run once.
#
# Produce the GIF:
#   vhs examples/demo/keel.tape
# Output:
#   examples/demo/keel.gif

Output examples/demo/keel.gif

Set FontSize 16
Set Width 960
Set Height 480
Set TypingSpeed 20ms
Set PlaybackSpeed 1.4
Set Theme "Dracula"

# Silently capture claude-code's lock id, then surface the ACQUIRED line in
# the CLI's usual shape so the GIF reads naturally.
Hidden
Type "LOCK=$(keel acquire ./fixture payments.ts claude-code --plan='add logging to chargeCard' --eta=6 --json | jq -r .lock_id)"
Enter
Sleep 200ms
Show

Type `echo "ACQUIRED  lock=${LOCK:0:8}  lease=5m  agent=claude-code"`
Sleep 300ms
Enter
Sleep 900ms

Type "keel acquire ./fixture payments.ts cursor --plan='refactor validate()'"
Sleep 300ms
Enter
Sleep 1400ms

Type "# claude-code finishes its edit..."
Enter
Sleep 400ms

Type "keel release $LOCK claude-code"
Sleep 300ms
Enter
Sleep 800ms

Type "keel acquire ./fixture payments.ts cursor --plan='refactor validate()'"
Sleep 300ms
Enter
Sleep 1200ms

Type "# coordinated by keel — 2 agents, 1 file, 0 conflicts"
Enter
Sleep 1500ms
