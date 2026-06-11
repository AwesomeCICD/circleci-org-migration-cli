# vhs tape for the circleci-migrate README demo.
# Regenerate with:  make demo-gif   (or: vhs docs/demo.tape)
# Deterministic + offline: shows version, the export->review->sync workflow,
# and the secure-secrets highlight via --help. No network, no org data.

Output docs/demo.gif

Set Shell "bash"
Set FontSize 17
Set Width 1200
Set Height 760
Set Padding 18
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms

# `make demo-gif` builds the binary and puts ./bin on PATH before invoking vhs,
# which the recording shell inherits — so commands read as plain `circleci-migrate`.
Hide
Type "clear"
Enter
Show

Sleep 800ms
Type "circleci-migrate version"
Sleep 400ms
Enter
Sleep 1500ms

Type "# Migrate a CircleCI org in two steps: export -> review -> sync"
Sleep 300ms
Enter
Sleep 700ms

Type "circleci-migrate --help"
Sleep 400ms
Enter
Sleep 4s

Type "clear"
Enter
Sleep 300ms

Type "# Secrets are encrypted by default - never written as plaintext artifacts"
Sleep 300ms
Enter
Sleep 700ms

Type "circleci-migrate secrets capture --help | sed -n '/ENCRYPTION/,/generate-key to have/p'"
Sleep 400ms
Enter
Sleep 4s

Type "clear"
Enter
Sleep 300ms

Type "# Prefer a guided flow? Run migrate with no flags for an interactive walkthrough."
Sleep 300ms
Enter
Type "circleci-migrate migrate --help | sed -n '1,14p'"
Sleep 400ms
Enter
Sleep 3500ms
