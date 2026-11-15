## 1.2.0-go

- Application exits when the last browser tab closes (six-second grace). Added Quit application button and headless opt-out; cancels in-progress analysis.

# Changelog

## v1.1.0-go — persistent credentials and stable chessboard

- Replaced the movable Unicode/grid inspector board with one 64-square, fixed-coordinate SVG board using embedded SVG pieces. It remains stable after move navigation and Flip, including in standalone exported dashboards.
- Added persistent optional Lichess token storage (macOS Keychain; private user config on Windows/Linux) with Save / Forget buttons and metadata-only status endpoint.
- Made the backend pick up saved credentials for both GUI and CLI without including them in analysis JSON, logs or reports.
- Restricted bearer-token forwarding to `https://lichess.org`; Opening Explorer queries never carry it.
- Added tests for credential isolation and persistence, and updated screenshots, documentation and release scripts.
- Preserved the Analysis / Opening Tree tabs, existing algorithms, exports and native icon branding.


## v0.2.0-go — unified workspace and native app icons

- Combined the PGN analysis setup and opening graph into **Analysis** / **Opening Tree** tabs in one browser application.
- Made both tabs fit in the browser viewport without page-level scrolling. Large result tables and detailed popovers retain internal scrolling.
- Added embedded graph subviews for **Opening tree**, **Lines to memorize**, **Practical traps**, and **Statistics & positions**, while keeping downloaded standalone dashboards functional.
- Added automatic local Stockfish detection, Windows/macOS/Linux-specific installation instructions, manual executable-path recheck, and disabling of local engine-only controls when no engine exists.
- Added native app icon resources: macOS `.icns` in `.app`, Windows PE `.rsrc` and NSIS installer `.ico`, Linux desktop launcher/icon.
- Updated the concise README with three actual screenshots of the new UI.
- Kept the existing Go PGN parser, analysis, report generation, tests and CLI interface.

## v0.1.0-go — initial Go port

- Introduced standalone Go backend and embedded frontend, Lichess Explorer/Cloud support, optional local UCI Stockfish, interactive graph reports and multi-platform release packaging.
