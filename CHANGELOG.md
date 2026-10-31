# Changelog

## v0.1.0-go — Go backend preview

- Replaced the Python runtime with a native, standard-library-only Go implementation.
- Embedded both HTML interfaces and the ChessPrune logo into the compiled executable.
- Implemented standard-chess PGN parsing (RAV variations, comments, NAGs, FEN, legal SAN) and deterministic repertoire selection.
- Added Lichess Opening Explorer caching, Cloud evaluation and optional Stockfish UCI integration.
- Added Go analysis server, progress reporting and standalone HTML/PGN outputs.
- Added Apple Silicon / Intel macOS, Windows x64 and Ubuntu/Linux x64/ARM64 release builds.
- Added MIT license, multi-platform packaging documentation and executable-free test suite.

**Pre-release note:** live network/engine analysis and cross-platform execution require further validation.
