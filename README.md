# ChessPrune

![ChessPrune — Less memorization. Smarter preparation.](assets/ChessPrune_SmarterPreparation.png)

<p align="center"><strong>Less memorization. Smarter preparation.</strong></p>

**ChessPrune is a local, browser-based tool for pruning large chess-opening repertoires into a focused set of variations worth studying.**

ChessPrune analyzes opening positions using the [Lichess Opening Explorer](https://explorer.lichess.ovh/) database and optional chess-engine evaluations, ranks positions by their practical study value, and lets you explore and edit the resulting move tree before exporting a smaller PGN.

It works with repertoires for **White or Black**. The frontend is HTML, CSS and vanilla JavaScript; the backend is Python. There is no hosted service, user account, frontend build step or web-framework dependency.

> **AI-assisted development and image-generation disclosure**  
> ChessPrune was created through successive design, coding and review iterations with **ChatGPT by OpenAI**. The project owner defined the requirements, provided feedback and tested iterations; **GPT-6 in ChatGPT assisted with generating and refining source code, UI design, documentation and tests**. The two original graphical assets shipped with this repository — [`ChessPrune_Icon.png`](assets/ChessPrune_Icon.png) and [`ChessPrune_SmarterPreparation.png`](assets/ChessPrune_SmarterPreparation.png) — were also **generated using ChatGPT's image-generation functionality and refined through iterative prompts**. This is a user-led independent project, not an official OpenAI, Lichess, Stockfish or Chessquid product. AI-assisted output may contain errors; please review the source and chess recommendations critically.

## At a glance

- **Two repertoire perspectives:** score, trap detection and whose moves are repertoire decisions adjust for White or Black.
- **One PGN, multiple chapters:** import a standard-chess PGN, including exported studies with variations, comments and numeric annotation glyphs (NAGs).
- **Evidence from actual games:** choose Lichess rating bands and speeds; retrieve game counts, results and relative frequencies.
- **Optional objective evaluation:** local Stockfish, Lichess Cloud evaluations, a hybrid approach or no engine.
- **Two selection strategies:** specify a **Target Lines** memorization budget (for example, 50 terminal variations), or use an opponent-response **Coverage** threshold.
- **Interactive results:** horizontal SAN move tree, full-move ruler, chapter/all-chapters/traps views, pan/zoom, Fit view, collapsible non-branching sequences, comment markers and position details.
- **Manual control:** include or exclude complete subtrees; manual decisions take precedence over automatic filtering when exporting.
- **Export:** filtered PGN, a PNG of the current graph and a sortable study-lines CSV.
- **Local execution:** the browser controls a Python server bound to `127.0.0.1`; generated dashboards are self-contained HTML files.

## Requirements

- Python **3.10+** (a recent 3.11–3.13 release is recommended).
- A modern browser (Chrome/Chromium recommended; other evergreen browsers may work).
- The Python dependencies listed in `requirements.txt`:
  - `python-chess` — PGN parsing, chess positions and UCI integration.
  - `requests` — Lichess HTTP requests.
- **Optional:** Stockfish installed locally if you select *Local*, *Hybrid*, or *Auto* with local-engine availability.
- Internet access for uncached Lichess Explorer / Cloud queries. With offline mode, the analyzer uses cached data only.

### macOS: installation

```bash
# Run these commands from the unpacked project directory.
python3 -m venv .venv
.venv/bin/python -m pip install -r requirements.txt

# Optional, but recommended for full engine coverage:
brew install stockfish

# Start the app:
.venv/bin/python app.py
```

The browser normally opens at **http://127.0.0.1:8765**. If that port is occupied, the server tries the next available port and prints its address. Leave the terminal running while using the analysis form. Press **Ctrl+C** to stop the server.

On Linux, install Stockfish with your package manager if needed; set its executable path in the analysis form if it cannot be discovered automatically. The local web server works on supported Python platforms, though primary manual testing has focused on macOS and Chromium.

**Subsequent launches** only require:

```bash
.venv/bin/python app.py
```

No `.sh` launcher, Node.js, npm, Flask or frontend compilation is required.

## Typical workflow

1. **Select a PGN.** The input is a repertoire tree, not necessarily a collection of played games. Its chapters and authored move order matter.
2. **Choose White or Black.** This determines which moves are *your repertoire choices* and which are the opponent's responses.
3. **Configure the evidence.** Select Lichess rating bands and time controls. These ratings are Lichess ratings, not FIDE ratings.
4. **Choose evaluations.** *Auto* prefers local Stockfish when installed and otherwise uses Cloud. *Local* requires Stockfish; *Cloud* may be missing many evaluations; *Hybrid* tries Cloud then local; *None* disables engine-based insights.
5. **Choose a selection strategy.** A practical starting point is **Target Lines = 50**, minimum **2 per chapter**, **1 repertoire-side option**, with trap priority **1×**. You can change this later in the results dashboard.
6. **Run a Quick test** to analyze a chapter-balanced sample of complete lines, then choose **Full analysis** for your complete PGN. A backend-driven progress bar tracks the analysis phases; logs appear at the bottom.
7. **Open the results dashboard.** Browse chapters individually, all chapter trees at once or practical traps. Click a SAN node to view its board, Lichess stats, engine evaluation and original comments. Close the detail panel with **×** or **Esc**.
8. **Refine your study plan.** Adjust Target Lines / Coverage, use the node-level `+`/`−` controls to include/exclude branches, and apply the filter. Use the sortable *Lines to memorize* table to decide what to learn first.
9. **Export.** Save the filtered PGN for your chess application; optionally export a graph PNG and study-lines CSV. Export before closing the dashboard if you made manual changes.

The analysis form focuses on new PGN analyses, without a saved-job history or report-import section. Completed results are still written to local disk.

## How the analysis works

```text
PGN (chapters, variations, comments, NAGs)
                    |
          python-chess PGN parser
                    |
          position / variation tree
                    |
     Lichess Opening Explorer statistics  <-- local persistent cache
                    |
       optional Stockfish / Cloud evals  <-- local persistent cache
                    |
       importance and trap classification
                    |
       Target Lines / Coverage selection
                    |
      analysis report + reduced PGN + HTML dashboard
                    |
    browser exploration + manual branch overrides
                    |
               PGN / PNG / CSV
```

### 1. Position statistics

Every position is keyed by its chess state (the relevant FEN fields). The program queries Lichess Explorer for the chosen ratings/speeds and stores the returned counts for **White wins, draws and Black wins**. It also tries to use *move-specific* counts from the parent's Explorer response when available.

For the chosen repertoire side:

\[
\mathrm{score}_{\mathrm{side}} = \frac{\mathrm{wins}_{\mathrm{side}} + 0.5\,\mathrm{draws}}{\mathrm{total\ games}}
\]

This is the **empirical score**, not the probability of winning the next game. Positions without statistics are labeled as missing rather than assigned invented game counts.

### 2. Engine evaluation

When available, evaluations are stored in **centipawns from White's perspective**: positive scores favor White, negative scores favor Black. Score-based importance and trap detection account for the selected repertoire side. Depth-limited evaluations are estimates, not proof of best play.

The application caches API responses and local engine evaluations. An unchanged analysis may reuse work from earlier runs. Different rating/speed filters or higher Stockfish depth may require new work.

### 3. Importance score

Each node receives an approximate **0–100 study-priority score**, combining:

| Component | Weight | Purpose |
| --- | ---: | --- |
| Relative frequency | 55% | Prefer positions commonly reached in the chapter (log-scaled). |
| Practical results | 25% | Reward strong empirical scores for the chosen color, adjusted toward 50% for small samples. |
| Objective evaluation | 20% | Prefer positions favorable to the repertoire side according to the engine. |

A practical-trap bonus can add up to 10 points, capped at 100. Missing components contribute neutral values. **Importance is a heuristic priority for study**, not an objective measure of the move's chess quality, a win probability or an Elo estimate.

### 4. Traps: two different concepts

**Tactical errors:** a move by the opponent causes a sufficiently large engine-evaluation swing, and an available repertoire response verifies that it can be exploited. Thresholds are configurable in the Python CLI.

**Practical traps:** a position is approximately balanced according to the engine, yet has an unusually high observed score for the repertoire player over a minimum sample. The implementation applies sample shrinkage and a gap between observed/expected performance. These patterns are statistical, **not proof of a forcing tactic or causal superiority**. Opponent skill, selection effects, small samples and depth limitations matter.

The dashboard distinguishes these categories. Original PGN annotations such as `!`, `?`, `!!`, `??`, `!?`, `?!`, `=`, `+=`, `=+`, `+-` and `-+` are displayed where present; they are **author-supplied annotations**, not new engine judgements.

### 5. Target Lines (memorization budget)

The program enumerates eligible terminal root-to-leaf variations, respects the chosen number of alternatives at repertoire-side turns, and selects up to the requested count using a **deterministic greedy heuristic**.

The heuristic considers relative reach probability *among the opponent continuations represented in the PGN*, position importance, newly covered positions, sample size, trap opportunities and a diminishing preference for chapters already heavily represented. It first tries to give each chapter the configured minimum number of lines, if the budget permits.

The filtered repertoire is the **union of the selected paths**, preserving their PGN move order and comments. Fewer lines may be returned when the original PGN or configured constraints provide fewer eligible candidates.

This procedure is **not an exact global optimization** and does not guarantee that 50 lines cover 50% or any particular percentage of all online games. A chapter-balanced study plan is a practical preference, not a statistical theorem.

### 6. Coverage mode

When it is your turn, the filter follows the authored repertoire choice(s). At an opponent turn it considers the supplied continuations and retains branches by importance until the desired cumulative fraction of Explorer games is reached, subject to minimum-sample and importance criteria. Unknown statistics and certain verified traps receive special handling.

Coverage refers to a local selection rule over the analyzed tree; it should not be interpreted as a guarantee of total real-world opening coverage.

## Understanding the results dashboard

### Move graph

The graph flows **left to right**. Each node is a **SAN half-move** (`e4`, `Nf3`, `O-O`, etc.) accompanied by a light dot for a White move or a dark dot for a Black move. The overlay ruler shows **full move numbers**, not half-move indices. The graph has **no artificial shared “Repertoire” or “Traps” root**: in *All chapters* and *Traps* views, the real chapter roots form a forest.

Use the mouse to pan/zoom, or **Fit view** to restore the overview. Linear sequences without branches can be collapsed and expanded. Annotated positions, comments and detected traps remain available for exploration.

Selecting a node opens a **closable position panel** with a chessboard, statistics, move and pre-move comments, evaluation, importance and continuations. An icon on annotated SAN nodes indicates that a textual move comment is available.

The small `+`/`−` control on a node includes/excludes that branch and its descendants. These manual overrides are applied on top of the automatic filter and affect the PGN exported from the dashboard. They remain in the open browser page; they are **not persisted across reloads**.

### Study lines table

The *Lines to memorize* table contains one row per selected terminal variation. Click any heading to toggle ascending/descending sorting. Its key columns include:

- **Relative weight:** estimated conditional probability within the PGN opponent replies for the chapter. Repertoire-side moves are deliberate decisions rather than random outcomes.
- **Absolute weight:** an indicative fraction of Lichess games from the chapter's starting position, computed as the product of consecutive observed move/parent-game ratios (each capped at 1). It can become very small in deep lines, and missing counts show `n/a`.
- **Games / Score / White eval:** sample size, selected-side empirical score and White-perspective engine evaluation.

Absolute weights are **not additive across chapters**, particularly if chapters overlap in their opening moves. Neither weight is a probability that the opponent will reach your chosen line given a different repertoire decision.

### Exports and files

Each analysis creates a timestamped directory under `analyses/`, containing the input PGN, a job metadata file, `console.log`, and a `results/` folder. The generated file and directory names are in English:

**Migration from older releases:** previous directories named `analisi/` or `risultati/` are not renamed or deleted automatically. New runs use `analyses/` and `results/`. To view an older compatible report, you can run `rebuild_dashboard.py` against its JSON file, regardless of the filename, without repeating Lichess or Stockfish queries.

| File | Description |
| --- | --- |
| `results/dashboard.html` | Standalone interactive graph/dashboard with embedded analysis data. |
| `results/data.json` | Machine-readable analysis report (nodes, stats, evals, comments and configuration). |
| `results/filtered_repertoire.pgn` | Initial PGN filtered during Python analysis. |
| `results/target_repertoire_<N>_lines.pgn` | Server-generated target-lines PGN, when the target strategy is active. |
| `console.log` | Human-readable backend execution messages. |

The dashboard can also export a **new PGN reflecting manual overrides**, a **PNG** of the current graph and a **CSV** of the study lines in current sort order. These download into your browser's configured downloads location.

## Architecture and source files

```text
ChessPrune/
├── app.py                   # Local HTTP server, job management, progress, file endpoints
├── web/
│   └── index.html           # English configuration / processing UI
├── repertoire_optimizer.py  # PGN parsing, Explorer, engine, traps, reports
├── line_selector.py         # Deterministic Target Lines selection
├── pgn_from_report.py       # PGN reconstruction from an existing JSON report
├── rebuild_dashboard.py     # Offline HTML/target-PGN rebuild utility
├── dashboard_template.html  # Self-contained interactive graph template
├── branding.py              # Embed ChessPrune icon in offline dashboards
├── assets/
│   ├── ChessPrune_Icon.png
│   └── ChessPrune_SmarterPreparation.png
├── requirements.txt
├── .gitignore
├── START_HERE.txt
└── README.md
```

**Python backend.** `app.py` serves the frontend via Python's built-in HTTP server, receives PGN uploads, validates parameters, starts the analyzer in a subprocess and tracks structured progress events. Jobs are run sequentially to limit CPU/memory usage and API load. HTTP routes are restricted to localhost with origin/host checks.

**HTML frontend.** `web/index.html` configures analyses and displays progress/logs. The generated `dashboard.html` contains its dataset inline, so graph exploration, filtering and exports do **not** call Python, Stockfish or Lichess again.

**Data boundary.** PGN files, reports and cached data remain on the machine by default, but **positions, selected ratings/speeds, and possibly a token** are sent to Lichess when network queries are enabled. A token supplied through the UI is passed to the subprocess as an environment variable, not written intentionally to job metadata; nevertheless, review logs and source before using secrets.

### Advanced use without the web UI

The backend analyzer can also be run directly:

```bash
.venv/bin/python repertoire_optimizer.py --help

.venv/bin/python repertoire_optimizer.py my_openings.pgn \
  --out output \
  --side white \
  --ratings 1800,2000 \
  --speeds blitz,rapid \
  --filter-mode target \
  --target-lines 50 \
  --min-chapter-lines 2 \
  --eval-source auto \
  --max-evals 0
```

If you already have a compatible report JSON, rebuild the dashboard without network or engine calls:

```bash
.venv/bin/python rebuild_dashboard.py path/to/data.json \
  --out path/to/dashboard.html \
  --target-lines 50
```

This rebuild utility remains available to advanced users even though the report-import section is not present in the main web UI.

## Development checks (optional)

The repository includes two small regression-test suites:

```bash
python3 -m unittest discover -s tests -v
node tests/test_forest_graph.js
```

The Python tests use the standard library. The JavaScript test uses **Node.js only for development checks**; Node is not necessary to run the application. These are smoke/regression tests, not a substitute for a complete live Lichess and Stockfish integration test on your own PGN.

## Troubleshooting

| Symptom | What to check |
| --- | --- |
| Browser page does not open | Read the URL printed in the terminal; try it manually. Keep `app.py` running. |
| `Missing Python dependencies` | Use the same Python environment for installation and launch: `.venv/bin/python -m pip install -r requirements.txt`. |
| Stockfish not found | Run `which stockfish`, install with `brew install stockfish`, specify an absolute path or select Cloud/None. |
| Long analyses / HTTP 429 | Lichess rate limits are real; the client backs off. Reuse the persistent cache, run Quick test first and avoid unnecessary reruns. |
| Cloud eval unavailable | Many positions have no published Cloud evaluation. Select local Stockfish or Hybrid for greater coverage. |
| Progress bar stays on one phase | A large PGN, engine search or Lichess cooldown may take time. Check **Backend logs** for details; the percentage tracks steps, not elapsed time. |
| Graph seems empty in Traps view | No positions may meet the selected practical-trap definition, or view filters may exclude them. Switch to All chapters. |
| Manually excluded branches return | Overrides exist in browser memory only. Export your customized PGN before reloading the page. |
| Output files are not listed in Git | Deliberate: `.gitignore` excludes analyzed PGNs, reports, logs, cache and virtual environments. |

## Publishing in an existing GitHub clone

The repository is already cloned at `/Users/fversari/WORKDIR/ChessPrune`. **Do not run `git init` or `gh repo create` again**. Unpack this release elsewhere and copy its contents (including dotfiles) **into the existing clone**, without replacing the `.git/` directory.

```bash
# Example: unpack into Downloads first
cd ~/Downloads
unzip ChessPrune_release.zip

# Copy source files and images, preserving the existing Git history.
rsync -av --exclude='.git' --exclude='.venv' --exclude='analyses' \
  ~/Downloads/ChessPrune_release/ \
  /Users/fversari/WORKDIR/ChessPrune/

cd /Users/fversari/WORKDIR/ChessPrune
# Review before committing (especially important when updating an existing clone):
git remote -v
git status --short
git diff --check
git diff --stat

# Stage source, docs and images (ignored PGNs and results stay untracked):
git add -A
git diff --cached --check
git diff --cached --stat

git commit -m "Rebrand as ChessPrune and add English UI and artwork"
git push -u origin "$(git branch --show-current)"
```

If the clone has source files removed or moved in the new release, inspect those changes in `git status` before the commit. If other contributors may have pushed newer commits, fetch/pull before overwriting local files. The included `.gitignore` excludes PGNs, user analyses, caches, logs and credentials, but **does not retroactively untrack previously committed files**.

**License:** A `LICENSE` file is intentionally not imposed by this release. If your GitHub repository already has a license, keep it. MIT is a straightforward permissive choice for a new repository; add the copyright holder and year only with the owner's approval.

## Limitations and acknowledgments

- The input is a **standard-chess PGN**. Unsupported variants, invalid PGNs, unusual custom-start positions and complex annotation encodings may need additional testing.
- The tool evaluates **PGN positions**, not every possible move in the chess opening universe. Repertoire choices normally follow the author's first listed move; selecting additional options expands candidate lines.
- Lichess frequency and performance depend on the selected population and are not necessarily predictive for a particular player.
- Engine depth, incomplete Cloud coverage and transpositions limit interpretation of evaluations and trap classifications.
- **Target Lines** uses a greedy ranking heuristic; it does not solve an exact optimal coverage problem.
- The local server is for a **single trusted user on their own computer**, not for internet-facing deployment or multi-user hosting.
- This project uses public Lichess services subject to their own availability, API usage guidance and rate limits. It uses the independently installed Stockfish engine through UCI; Stockfish is not bundled.
- The interface and graphics were developed iteratively with ChatGPT. Similarities to other chess-tree visualization tools reflect familiar interaction patterns, not affiliation or use of their proprietary code.

**Acknowledgments:** ChatGPT by OpenAI (AI-assisted code and generated imagery), [Lichess](https://lichess.org/) and its Opening Explorer, [Stockfish](https://stockfishchess.org/), [python-chess](https://python-chess.readthedocs.io/), and the authors/maintainers of the open-source libraries used by this project.

### Brand artwork and reuse

The two PNG files in `assets/` are part of the ChessPrune branding. The configuration interface serves `ChessPrune_Icon.png` from the loopback HTTP server; each generated standalone HTML dashboard embeds that same image as a data URI, so it also works offline. The repository banner at the top of this README uses `ChessPrune_SmarterPreparation.png`. If you redistribute or modify the assets, check that your intended license and attribution cover the images as well as the source code.
