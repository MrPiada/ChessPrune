"""Build target PGNs from an analyzed data.json without chess/Stockfish/network.

The original SAN, annotations, NAGs and header values are retained. Tree
structure and variation order are identical to the source PGN.
"""
from __future__ import annotations

from pathlib import Path
import re
from line_selector import select_target_lines


def write_target_pgn(report: dict, destination: Path, target: int = 50,
                     min_per_chapter: int = 2, min_games: int = 20,
                     trap_boost: float = 1.0) -> dict:
    nodes, roots = report['nodes'], report['roots']
    byid = {n['id']: n for n in nodes}
    cfg = report['config']
    result = select_target_lines(nodes, roots, target, min_per_chapter,
                                 min_games, trap_boost, cfg.get('repertoire_options', cfg.get('black_options', 1)),
                                 cfg.get('side', 'black'))
    keep = result['kept']

    def comment(s: str) -> str:
        return ('{ ' + re.sub(r'[\r\n]+', ' ', str(s).replace('{', ' ').replace('}', ' ')) + ' } ') if s else ''

    def move(node: dict) -> str:
        parent = byid[node['parent']]
        fields = parent['fen'].split()
        no = int(fields[5]) if len(fields) >= 6 else 1
        prefix = f'{no}.' if parent['turn'] == 'white' else f'{no}...'
        nags = ''.join(f'${n} ' for n in node.get('nags', []))
        return (prefix + ' ' + comment(node.get('starting_comment', ''))
                + node['san'] + ' ' + nags + comment(node.get('comment', '')))

    def line(node_id: int) -> str:
        return move(byid[node_id]) + continuation(node_id)

    def continuation(node_id: int) -> str:
        children = [i for i in byid[node_id]['children'] if i in keep]
        if not children:
            return ''
        output = move(byid[children[0]])
        for child_id in children[1:]:
            output += '( ' + line(child_id) + ') '
        return output + continuation(children[0])

    games = []
    for root in roots:
        if root not in keep:
            continue
        chapter = byid[root]['chapter']
        headers = report['headers'][chapter] if chapter < len(report['headers']) else {}
        def header(k,v):
            key = re.sub(r'[^A-Za-z0-9_]', '', str(k))
            value = str(v).replace('\\', '\\\\').replace('"', '\\"')
            return f'[{key} "{value}"]'
        header_block = '\n'.join(header(k,v) for k,v in headers.items())
        body = comment(byid[root].get('comment', '')) + continuation(root) + ' ' + headers.get('Result', '*')
        games.append(header_block + '\n\n' + body.strip() + '\n')
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text('\n'.join(games), encoding='utf-8')
    return result
