# circleci-migrate demo — no network/tokens required
# Demonstrates the key commands and help output

Output docs/demo.gif

Set FontSize 18
Set Width 1200
Set Height 700
Set Theme "Dracula"
Set Shell "bash"
Set Padding 20
Set Margin 10
Set BorderRadius 10

# Use the locally-built binary
Type "export PATH=/tmp:$PATH" Enter
Sleep 500ms

# Top-level help
Type "circleci-migrate --help" Enter
Sleep 2s

# migrate help — shows interactive vs flag-driven paths
Type "circleci-migrate migrate --help" Enter
Sleep 2s

# secrets capture help — shows guided/encryption options
Type "circleci-migrate secrets capture --help" Enter
Sleep 2s

# version
Type "circleci-migrate version" Enter
Sleep 1s
