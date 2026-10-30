"""Target-lines selector: deterministic greedy selection on the original PGN tree.

Only opponent continuations branch. For the repertoire side, follow the first
authored move (REPERTOIRE_OPTIONS=1 is recommended). Each selected original leaf represents one
complete study line; the filtered PGN is the union of selected root-to-leaf paths.
This is a heuristic, not an exact combinatorial optimum or probability of winning.
"""
from __future__ import annotations

import math
from collections import defaultdict


def select_target_lines(nodes: list[dict], roots: list[int], target: int = 50,
                        min_per_chapter: int = 2, min_games: int = 20,
                        trap_boost: float = 1.0, repertoire_options: int = 1,
                        side: str = "black") -> dict:
    """Return keys kept, leaf_ids, per_chapter, available, actual_target.

    The node list is indexed by node['id'], as emitted by repertoire_optimizer.
    """
    byid = {n['id']: n for n in nodes}
    prob: dict[int, float] = {}
    candidates: dict[int, list[tuple[int, ...]]] = defaultdict(list)

    def game_count(node):
        stats = node.get('stats') or {}
        n = stats.get('n')
        return n if isinstance(n, (int, float)) and math.isfinite(n) and n > 0 else 0

    def walk(idx: int, path: tuple[int, ...], probability: float, chapter: int) -> None:
        n = byid[idx]
        path += (idx,)
        prob[idx] = probability
        kids = n.get('children') or []
        if not kids:
            candidates[chapter].append(path)
            return
        if n['turn'] == side:
            for child in kids[:max(1, repertoire_options)]:
                walk(child, path, probability, chapter)
        else:
            # Opponent moves are weighted RELATIVE TO OTHER OPPONENT MOVES IN THE PGN,
            # not all moves in the Lichess opening explorer.
            weights = [game_count(byid[c]) for c in kids]
            weights = [w if w > 0 else 1.0 for w in weights]
            total = sum(weights)
            for child, w in zip(kids, weights):
                walk(child, path, probability * w / total, chapter)

    for chapter, root in enumerate(roots):
        walk(root, (), 1.0, chapter)

    cap = min(max(0, int(target)), sum(len(v) for v in candidates.values()))
    selected: set[int] = set()
    chosen_leaves: list[int] = []
    counts = {ch: 0 for ch in range(len(roots))}
    taken: set[int] = set()

    def marginal(path: tuple[int, ...], chapter: int) -> float:
        new = [byid[idx] for idx in path if idx not in selected]
        if not new:
            return -1.0
        main = sum(prob[n['id']] * (0.60 + 0.40 * max(0, min(100, n.get('importance') or 0)) / 100)
                   for n in new) / len(new)
        main *= 1.0 + 0.10 * math.log2(1 + len(new))
        # A small sample should not win on statistical noise alone.
        first_n = game_count(new[0])
        rarity = 0.30 + 0.70 * min(1.0, first_n / max(1, min_games))
        bonus = 0.0
        for n in new:
            if n.get('practical_trap') or (n.get('trap') or {}).get('confirmed'):
                confidence = math.sqrt(game_count(n) / (game_count(n) + 100))
                reach = max(0.15, prob[n['id']] ** 0.30)
                bonus += 0.08 * confidence * reach * (1 if n.get('practical_trap') else 0.65)
        fairness = 1.0 / (1.0 + 0.12 * counts[chapter])
        return (main * rarity + trap_boost * bonus) * fairness

    def choose(chapter: int | None = None) -> bool:
        best = None
        best_key = None
        for ch in range(len(roots)):
            if chapter is not None and chapter != ch:
                continue
            for path in candidates.get(ch, []):
                leaf = path[-1]
                if leaf in taken:
                    continue
                gain = marginal(path, ch)
                key = (gain, -leaf)
                if best_key is None or key > best_key:
                    best_key, best = key, (ch, path)
        if best is None:
            return False
        ch, path = best
        selected.update(path)
        taken.add(path[-1])
        chosen_leaves.append(path[-1])
        counts[ch] += 1
        return True

    # First distribute places evenly across nonempty chapters. If target is
    # smaller than number of chapters, not every chapter can be represented.
    for _ in range(max(0, int(min_per_chapter))):
        for ch in range(len(roots)):
            if len(taken) >= cap:
                break
            if counts[ch] < len(candidates.get(ch, [])):
                choose(ch)
    while len(taken) < cap:
        if not choose():
            break

    return {'kept': selected, 'leaf_ids': chosen_leaves, 'per_chapter': counts,
            'available': sum(len(v) for v in candidates.values()),
            'actual_target': cap}
