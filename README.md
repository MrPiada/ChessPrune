<p align="center">
  <img src="assets/ChessPrune_SmarterPreparation.png" alt="ChessPrune — Less memorization. Smarter preparation." width="100%">
</p>

# ChessPrune

**Less memorization. Smarter preparation.**

ChessPrune is a free, open-source, local-first chess opening repertoire optimizer. Import a PGN (including multi-chapter Lichess studies), use [Lichess Opening Explorer](https://explorer.lichess.org/) game frequencies and optional chess-engine evaluations to identify the lines worth learning, explore the annotated opening tree, and export a reduced PGN.

**Now implemented in Go:** one native executable per operating system, with the complete HTML/JavaScript frontend embedded at build time. Running the application requires **no Python, pip, Node.js, Go installation, or web hosting**. You do need a modern browser. All chess analysis logic, PGN parsing, data selection, HTTP handling and file export is implemented in Go.

> **Release maturity:** The Go port was validated with PGN parsing tests, offline end-to-end analysis, server endpoint tests, a line-selection comparison against the original Python version and cross-compilation checks. This is a pre-release: live Lichess/Stockfish analysis and execution of every cross-compiled binary still need independent platform testing. Please report reproducible issues.

## Download and run

Download the archive or application for your platform from **GitHub → Releases** (assets must first be published by the project maintainer).

| Platform | Distribution | Launch |
|---|---|---|
| macOS Apple Silicon | `ChessPrune-vX-darwin-arm64.zip` or `.dmg` | Open `ChessPrune.app` |
| macOS Intel | `ChessPrune-vX-darwin-amd64.zip` or `.dmg` | Open `ChessPrune.app` |
| Windows x64 | `ChessPrune-vX-windows-amd64.zip` | Unzip and double-click `ChessPrune.exe` |
| Ubuntu/Linux x64 | `ChessPrune-vX-linux-amd64.tar.gz` | Extract, then run `./ChessPrune` |
| Ubuntu/Linux ARM64 | `ChessPrune-vX-linux-arm64.tar.gz` | Extract, then run `./ChessPrune` |

ChessPrune starts a local server at `http://127.0.0.1:8765` (or the next available port), then opens the browser automatically. Leave the application running while interacting with the analysis page. Stop it with `Ctrl+C` when using a terminal. Windows may display a console window while the local server runs.

**Unsigned app warning:** macOS and Windows may warn about downloaded unsigned applications. These archives are not Apple-notarized or Microsoft Authenticode-signed. Never disable platform security protections globally; verify release checksums and the source before running downloaded software. For public distribution without prominent security warnings, the publisher should sign and notarize the macOS build and code-sign the Windows executable/installer.

### What's inside the executable?

- Go HTTP server listening on `127.0.0.1` only
- A static, embedded HTML analysis frontend (no CDN)
- Native Go parser for standard-chess PGN, including variations, FEN, comments and NAGs
- Analysis against the Lichess Opening Explorer API with caching and retry/backoff for HTTP 429
- Lichess Cloud evaluation integration and a UCI client for an **optional** local Stockfish engine
- Statistical scoring, practical-trap classification, an importance heuristic and line filtering
- A complete standalone HTML result dashboard, with graph browsing, annotation and manual inclusion/exclusion, PGN/PNG/CSV export

ChessPrune itself is self-contained. **Stockfish is NOT bundled**: local engine analysis requires a separately installed executable. Select **Auto** (uses Stockfish if present, otherwise Lichess Cloud), **Cloud**, or **None** to run without a local engine. Internet access is still required for uncached Explorer/Cloud requests.

## Quick start from source

Only the **Go toolchain (1.23+)** is needed to *compile* ChessPrune.

```bash
git clone https://github.com/YOUR_USERNAME/ChessPrune.git
cd ChessPrune
go test ./...
go run .
```

To create a single executable for your current computer:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o ChessPrune .
./ChessPrune
```

For Windows, use `ChessPrune.exe` as the output name.

### Run without the browser GUI

```bash
./ChessPrune --analyze my_openings.pgn --side white \
  --target-lines 50 --eval-source auto --out results
```

Other CLI switches include `--eval-source none`, `--offline`, `--port`, `--no-browser` and `--version`. The browser GUI exposes more configuration controls, including rapid/blitz ratings, Stockfish depth, caches, trap priority, target line count and coverage thresholds.

## How it works

### 1. Load an authored PGN repertoire

ChessPrune preserves move-tree order and original comments while interpreting *standard chess* legal moves, castling, promotions, en passant, FEN starting positions, nested variations and NAG annotations. Unsupported variants such as Chess960 are rejected. The repertoire color controls whose moves represent author choices and whose moves represent potential opponent replies.

### 2. Query Lichess statistics

Game counts come from Lichess Opening Explorer, filtered by rating band and time control. For positions with multiple transpositions, ChessPrune caches Explorer requests by their canonical four-field FEN. Per-move counts are read from the parent's Explorer response when available.

ChessPrune attempts to reuse existing Python-era Explorer cache entries if they are in `~/.cache/repertoire_optimizer/` and have matching query settings. On macOS and Linux this directory is in the user's home folder; using a Go binary does not erase the previous cache. Lichess rate limiting is respected: 429 responses cause a cooldown of **at least 60 seconds**. The tool does not collect account credentials by default; any optional Lichess token is kept only for the running analysis.

### 3. Optionally evaluate with Stockfish

ChessPrune communicates with a separately installed Stockfish engine over the [UCI protocol](https://www.chessprogramming.org/UCI), or requests available evaluations from Lichess Cloud. In **Auto** mode, Stockfish takes precedence when found. Evaluation values in the report are normalized to the **White point of view** (positive values favor White). The selected repertoire side is used when ranking and classifying traps.

Cloud evaluations can be missing for uncommon positions. Local evaluations depend on the chosen depth and engine build. No engine evaluation is proof that a variation is the only correct continuation.

### 4. Estimate position importance

The heuristic uses a mixture of:

- **55% frequency:** game counts logarithmically normalized within each chapter
- **25% practical score:** empirical game results for your color, shrunk toward 50% for small samples
- **20% objective evaluation:** engine centipawns transformed to a smooth normalized score

An additional bonus can highlight statistically interesting practical traps. These are approximately balanced positions with unusually strong real-game results for the repertoire side; **a practical trap is an observation, not a forced tactical refutation**.

### 5. Prune the repertoire

**Target Lines** chooses a fixed study budget (for example 50 complete variations). Candidate lines are scored by incremental usefulness, relative frequencies of the opponent's *authored PGN* continuations, importance, chapter balancing and adjustable trap priority. This is a deterministic **greedy heuristic**, not an exact optimal solver. You can set a minimum allocation per chapter.

**Coverage** preserves the author's first N candidate moves for their side and selects opponent responses until the configured per-position statistical coverage is reached. Missing game counts are never silently fabricated. The full repertoire remains accessible in the input file.

You can also manually include/exclude whole subtrees in the result GUI. The final exported PGN follows those manual selections.

### 6. Explore and export

The standalone dashboard offers:

- Horizontal move tree with SAN notation (`e4`, `Nf3`), full-move axis and White/Black move dots
- Collapsible unbranched chains, all-chapter view, trap view and move comments
- Click-to-open position details, including chessboard, evaluations and statistics
- Node importance colors, NAG and qualitative annotations
- An adjustable Target Lines / Coverage selector and clickable inclusion/exclusion controls
- Sortable *Lines to memorize* table with relative and absolute estimated weights
- PGN, CSV and PNG exports

The result HTML file includes the data and ChessPrune logo, so it opens **offline** in a normal browser without the Go server. For revised Lichess/Stockfish statistics, start a fresh analysis from the app.

## Compilation and release distribution (build everything on macOS)

Install the current Go toolchain (for example `brew install go`) and optionally the GitHub CLI (`brew install gh`). From the repository root on your Mac:

```bash
cd /Users/fversari/WORKDIR/ChessPrune
go version
go test ./...
./scripts/build-release.sh v0.1.0-go
ls -lh dist/
```

This script uses `GOOS`, `GOARCH`, and `CGO_ENABLED=0` and creates:

| Target | Generated release asset |
|---|---|
| macOS arm64 | ZIP of `.app`, and DMG when run on macOS |
| macOS amd64 | ZIP of `.app`, and DMG when run on macOS |
| Windows amd64 | ZIP containing `ChessPrune.exe`; optional NSIS `Setup.exe` |
| Linux amd64 | `.tar.gz` containing `ChessPrune` |
| Linux arm64 | `.tar.gz` containing `ChessPrune` |
| All targets | `SHA256SUMS.txt` |

Go's pure-Go cross compilation works on macOS without installing a Windows or Linux compiler. A macOS **DMG is generated by `hdiutil` and therefore requires macOS**, but no cross-target VM. **Build success is not runtime validation**: test the Windows executable on Windows and the Linux executable on Ubuntu, ideally in CI or VMs before marking a release stable.

On macOS, `brew install makensis` enables an **optional NSIS Windows `Setup.exe` installer** in addition to the portable ZIP; it does not require Windows to compile. This installer has not been tested on Windows in this environment. The native macOS `.app` packages and Windows installers/ZIPs are not code-signed/notarized. On macOS, an authenticated Developer ID certificate, Hardened Runtime, `notarytool`, and `stapler` are needed for a normal public notarized release. On Windows, SmartScreen reputation and code signing matter for a professional installer. On Linux, no AppImage is generated by default.

### Publish a release on GitHub

First commit the new Go project to your existing clone, then create an annotated tag and push it:

```bash
git status
git add -A
git diff --cached --check
git commit -m "Port ChessPrune backend to Go and embed the frontend"
git push origin main

git tag -a v0.1.0-go -m "ChessPrune native Go pre-release"
git push origin v0.1.0-go
```

Then, if you have GitHub CLI installed:

```bash
brew install gh  # only if not already installed
gh auth login    # only once
gh release create v0.1.0-go dist/ChessPrune-v0.1.0-go-* dist/SHA256SUMS.txt \
  --title "ChessPrune v0.1.0-go (preview)" --prerelease \
  --notes "Native Go backend, embedded web interface, and macOS / Windows / Linux builds. Stockfish optional."
```

Alternatively, on GitHub open **Releases → Draft a new release**, select tag `v0.1.0-go`, and drag in all the files from `dist/`. Select **Pre-release** until the binaries have been tested on each operating system.

For a later release, bump the tag (`v0.1.1-go`, etc.) and repeat the script; do not overwrite already published immutable release tags.

### Updating your existing local clone safely

Don't run `git init` again, and don't `rsync --delete` into the repository (it might delete `.git`). Verify a clean working tree first, then copy the new sources using `rsync -av` while excluding `.git`, `dist`, `analyses`, and local cache folders. Review the staged changes carefully before committing. Remove obsolete tracked `.py` files only after confirming that the Go sources replace them.

## Local data and security

Only local loopback (`127.0.0.1`) is bound by the server. The backend enforces local Host/Origin checks and prevents arbitrary file downloads. Uploaded PGN input, analysis results and backend logs are stored under the OS-specific **user configuration directory** (`ChessPrune/analyses`). The result HTML is self-contained; you may copy it elsewhere. Personal opening repertoires, cached Lichess statistics and output reports are excluded by `.gitignore`.

The server has no login and is intended for your own desktop, not a public network. It is **not** a hosted web service. PGNs and comments are user-controlled data; review any files you share.

## Development / testing

```bash
go test -v ./...
go vet ./...
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /tmp/ChessPrune-macos .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /tmp/ChessPrune.exe .
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/ChessPrune-linux .
```

The tests cover standard PGN parsing, comments/NAG retention, deterministic target selection, offline report generation and the HTTP server's analysis flow. They do not require network access or Stockfish.

## Limitations and acknowledgments

- The repertoire must be a **standard-chess PGN**. Chess960 and other variants are currently unsupported.
- Only moves present in the PGN can be considered. ChessPrune does not generate an optimal opening repertoire from scratch.
- Lichess player-pool statistics are not personalized winning probabilities. Rare lines suffer sampling noise.
- The practical-trap classifier identifies statistical disparities, not necessarily tactical traps.
- Newer Stockfish releases, Cloud positions, and varying hardware may produce different evaluation depth/results.
- Complete multi-platform tests and live API/engine tests are recommended before public "stable" releases.

ChessPrune uses the public [Lichess Opening Explorer](https://explorer.lichess.org/) and optional [Stockfish](https://github.com/official-stockfish/Stockfish). It is **not affiliated with or endorsed by Lichess, Stockfish, GitHub or OpenAI**.

### Development and AI attribution

**This project was created through extensive iterative collaboration with ChatGPT, including model GPT-6.** The product concept, requirements, feature priorities and iterative feedback were provided by the project maintainer. ChatGPT assisted with **generating and refining the Python prototype, Go code, HTML/JavaScript frontend, tests, build scripts and documentation**. The ChessPrune branding artwork, including `ChessPrune_Icon.png` and `ChessPrune_SmarterPreparation.png`, was **generated using ChatGPT's image-generation capability** and refined through user feedback. AI-assisted code should be reviewed and tested like any other code; the maintainers remain responsible for published releases and documentation.

## License

ChessPrune is released under the **MIT License**. See [`LICENSE`](LICENSE). Stockfish is an independently licensed **GPLv3** program and is not included in the ChessPrune executable. If you decide to redistribute Stockfish binaries in separate or combined packages, follow the Stockfish GPLv3 redistribution requirements, including the corresponding source and license.
