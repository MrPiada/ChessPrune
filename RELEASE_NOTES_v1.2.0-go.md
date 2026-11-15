# ChessPrune v1.2.0-go — Clean application shutdown

This release fixes the macOS/Windows/Linux behavior where closing the browser UI left the local Go server running indefinitely.

- **Quit application** in the app header: confirm, cancel the current analysis and stop ChessPrune.
- **Automatic shutdown** when the last ChessPrune browser tab closes, with a six-second refresh/reconnect grace period.
- **Multiple tabs supported**; closing one tab does not stop the app while another is open.
- Uses a persistent browser-to-server SSE connection and an explicit page-unload beacon as a fallback.
- **Headless CLI** (`--no-browser`) retains manual Ctrl+C shutdown. The `--analyze` CLI is unchanged.
- Existing SVG chessboard, persistent Lichess token, analysis logic, reports, exports and branding remain unchanged.

**Platform support:** macOS ARM64/Intel, Windows x64, Linux x64/ARM64. The build script generates macOS DMGs on a Mac and Windows NSIS installer when `makensis` is installed.

**QA:** Go tests, `go vet`, cross-builds, persistent-connection network lifecycle tests with actual Go processes. Browser-to-localhost navigation is restricted in the build environment; verify the final .app on your Mac before distributing broadly. Packages are unsigned/not notarized.
