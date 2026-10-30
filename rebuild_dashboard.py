#!/usr/bin/env python3
"""Rebuild the standalone HTML dashboard from an existing analysis report.

No Lichess requests, Stockfish invocation, or python-chess dependency.
Manual GUI selections are applied only when exporting from the dashboard.
"""
import argparse
import json
from pathlib import Path
from pgn_from_report import write_target_pgn


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("json", type=Path, help="Analysis report JSON produced by this project")
    parser.add_argument("--out", type=Path, default=None, help="Destination HTML file")
    parser.add_argument("--filter-mode", choices=("target", "coverage"), default="target")
    parser.add_argument("--target-lines", type=int, default=50)
    parser.add_argument("--min-chapter-lines", type=int, default=2)
    parser.add_argument("--trap-boost", type=float, default=1.0)
    args = parser.parse_args()
    report = json.loads(args.json.read_text(encoding="utf-8"))
    if not all(key in report for key in ("nodes", "roots", "headers", "summary", "config")):
        parser.error("Incomplete analysis report: required dashboard fields are missing.")
    report['config'].update(filter_mode=args.filter_mode, target_lines=args.target_lines,
                            min_chapter_lines=args.min_chapter_lines, trap_boost=args.trap_boost)
    template = Path(__file__).with_name("dashboard_template.html").read_text(encoding="utf-8")
    target = args.out or args.json.with_name("dashboard.html")
    payload = json.dumps(report, ensure_ascii=False, separators=(",", ":")).replace("</", "<\\/")
    target.parent.mkdir(parents=True, exist_ok=True)
    from branding import embed_icon
    html = template.replace("/*__EMBED_DATA__*/", "const DATA = " + payload + ";")
    target.write_text(embed_icon(html), encoding="utf-8")
    print(f"ChessPrune dashboard rebuilt: {target}")
    if args.filter_mode == 'target':
        target_file = target.parent / f'target_repertoire_{min(args.target_lines, len([n for n in report["nodes"] if not n["children"]]))}_lines.pgn'
        selection = write_target_pgn(report, target_file, args.target_lines,
                                     args.min_chapter_lines, report['config'].get('min_games', 20), args.trap_boost)
        actual = len(selection['leaf_ids'])
        if actual != min(args.target_lines, len([n for n in report['nodes'] if not n['children']])):
            renamed = target.parent / f'target_repertoire_{actual}_lines.pgn'
            target_file.rename(renamed)
            target_file = renamed
        print(f"Target PGN: {target_file} | lines {actual}, chapters {len(report['roots'])}")
    print(f"Nodes: {len(report['nodes'])} | chapters: {len(report['roots'])} | no Lichess/Stockfish calls.")


if __name__ == "__main__":
    main()
