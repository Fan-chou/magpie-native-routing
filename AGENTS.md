# Agent Safety Instructions

## AppleScript and System Events

These rules are mandatory after an earlier script accidentally sent macOS a
logout command.

1. Never name an AppleScript variable `out`. Use a neutral name such as
   `result_text`.
2. Never place `log out`, `restart`, `shut down`, or `sleep` inside a
   `tell application "System Events"` block. Put diagnostic logging outside
   the `tell` block.
3. Prefer avoiding System Events entirely. To inspect windows, use
   `CGWindowList` from a small Swift script or `screencapture`.
4. Before running any command that can affect the whole machine, ask the
   user first. This includes logout, restart, shutdown, `killall`, `pkill`
   of non-project processes, and equivalent platform commands.
