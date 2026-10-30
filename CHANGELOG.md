# Changelog

## ChessPrune — 2026-10-30

- Rebranded the application as **ChessPrune** in the analysis UI, graph UI, CLI and documentation.
- Integrated `assets/ChessPrune_Icon.png` into the analysis UI; embedded the same icon in standalone results dashboards.
- Added the `assets/ChessPrune_SmarterPreparation.png` project banner to the README.
- Standardized new job directories and output filenames in English (`analyses/`, `results/`, `data.json`, `filtered_repertoire.pgn`, `target_repertoire_<N>_lines.pgn`).
- Updated README and credited iterative ChatGPT-assisted coding and ChatGPT-generated visual artwork.
- Added branding smoke tests; preserved White/Black support, Target Lines, Coverage, traps, manual branch selection and PGN/PNG/CSV exports.

### Compatibility

This release changes the names of *newly generated output files*. Old analysis JSON reports can still be read by `rebuild_dashboard.py` without a new analysis. Existing local result directories are not moved automatically.
