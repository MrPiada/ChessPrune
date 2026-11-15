# ChessPrune v1.1.0-go

This release improves the desktop experience and privacy while preserving the Go analysis engine, target-line and coverage modes, and the unified Analysis / Opening Tree interface.

### Changes

- **Stable chessboard:** position details use one fixed-coordinate SVG board with bundled vector pieces, removing Unicode glyph and grid-size glitches. Flip, last-move highlighting and standalone HTML report support are retained.
- **Saved Lichess token:** optional token can be saved once and reused in future sessions and versions. Save / Forget in the Analysis tab; no secret is returned by the API or written to report JSON/PGN.
- **Per-user storage:** macOS Keychain; user configuration file (not encrypted) on Windows/Linux. `LICHESS_TOKEN` environment variable takes precedence.
- **Token routing:** the token is sent only to the secure `lichess.org` host when needed; never to `explorer.lichess.org`.
- **Tests and screenshots:** new persistence, API and token-isolation checks plus updated GUI screenshots.

### Requirements and limitations

- The app itself requires no Python runtime. Stockfish remains optional and is not bundled.
- Prebuilt files cover macOS (Apple Silicon / Intel), Windows x64, Linux x64 / ARM64. When built on a Mac, the release script also creates unsigned `.dmg` installers and, with NSIS installed, Windows `-Setup.exe`.
- Builds are not notarized or commercially code-signed. Validate on target devices before broad distribution.
- Live API requests and OS-specific Keychain/Windows operations require validation on the actual devices.
