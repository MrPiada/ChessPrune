ChessPrune — portable standalone application

ChessPrune contains a native Go backend and embedded HTML/CSS/JavaScript.
It does not require Go, Python, pip, or Node.js to run.

macOS: Open ChessPrune.app and use the web page that opens in your browser.
Windows: Double-click ChessPrune.exe (a console window hosts the local server).
Linux: In a terminal, run ./ChessPrune (make it executable if needed).

A modern browser is required. The server listens on 127.0.0.1 only.
Keep the application running while using its analysis page.
Analysis reports are saved under the operating system's user configuration directory.

For chess-engine analysis, install Stockfish separately or point the GUI to
its executable. Lichess Cloud evaluations require no Stockfish installation,
but they require network access and may not exist for all positions.

macOS/Windows installers are not code-signed/notarized by this build script.
See the GitHub README for security warnings, installation details and source.

APP LIFECYCLE
-------------
The application runs a local Go server while its browser UI is open.
Close the last application tab (allow a few seconds) or click "Quit application"
in the app header to shut down the server and cancel an active analysis.
A page refresh does not shut down the app. If started in headless mode
(--no-browser for ChessPrune, --no-open for ChessGrowth), stop with Ctrl+C.
