#!/usr/bin/env python3
"""Analyse and simplify a PGN repertoire against the Lichess opening database.

Dependencies: python-chess, requests (see requirements.txt).
Run 'python repertoire_optimizer.py --help' for the available settings.
"""
from __future__ import annotations

import argparse
import copy
import heapq
import html
import json
import logging
import os
import shutil
import sys
import time
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import chess
import chess.engine
import chess.pgn
import requests

LOGGER = logging.getLogger("repertoire")
EXPLORER = "https://explorer.lichess.org/lichess"
CLOUD = "https://lichess.org/api/cloud-eval"


def emit_progress(phase: str, done: int, total: int, percent: float, message: str = ""):
    """Structured, cumulative progress event consumed by the local web server.

    The percentages are phase-weighted, not estimates of remaining wall time:
    0..5 setup, 5..55 Explorer, 55..95 engine, 95..100 generation.
    """
    print("@@PROGRESS " + json.dumps({"phase": phase, "done": done, "total": total,
                                      "percent": round(percent, 1), "message": message},
                                     ensure_ascii=False), flush=True)


def pos_key(board: chess.Board) -> str:
    """Four FEN fields; ignores move clocks, so transpositions share API results."""
    return " ".join(board.fen().split()[:4])


def score(counts: dict | None, side: str = "black") -> float | None:
    if not counts:
        return None
    n = sum(int(counts.get(k, 0)) for k in ("white", "draws", "black"))
    return (counts.get(side, 0) + 0.5 * counts.get("draws", 0)) / n if n else None


def n_games(counts: dict | None) -> int | None:
    if counts is None:
        return None
    return sum(int(counts.get(k, 0)) for k in ("white", "draws", "black"))


class ApiClient:
    """Serial, cached HTTP requests; 429 follows Lichess's 60s minimum cooldown."""

    def __init__(self, cache_dir: Path, token: str, delay: float, offline: bool):
        self.dir = cache_dir
        self.dir.mkdir(parents=True, exist_ok=True)
        self.session = requests.Session()
        self.session.headers.update({"User-Agent": "ChessPrune/1.0 (personal PGN analysis)",
                                     "Accept": "application/json"})
        if token:
            self.session.headers["Authorization"] = f"Bearer {token}"
        self.delay = delay
        self.offline = offline
        self.last_request = 0.0
        self.network_calls = 0
        self.rate_limits = 0

    def fetch(self, service: str, url: str, params: dict[str, str | int]) -> dict | None:
        import hashlib
        payload = json.dumps({"service": service, "params": params}, sort_keys=True)
        key = hashlib.sha256(payload.encode("utf-8")).hexdigest()
        path = self.dir / f"{service}_{key}.json"
        if path.exists():
            return json.loads(path.read_text(encoding="utf-8"))
        if self.offline:
            return None
        for attempt in range(6):
            elapsed = time.monotonic() - self.last_request
            if self.last_request and elapsed < self.delay:
                time.sleep(self.delay - elapsed)
            self.last_request = time.monotonic()
            self.network_calls += 1
            try:
                response = self.session.get(url, params=params, timeout=(10, 45))
            except requests.RequestException as exc:
                if attempt == 5:
                    LOGGER.warning("Unable to request %s: %s", service, exc)
                    return None
                time.sleep(min(5 * (attempt + 1), 30))
                continue
            if response.status_code == 404 and service == "cloud":
                path.write_text("null", encoding="utf-8")
                return None
            if response.status_code == 429:
                # Official Lichess recommendation: wait AT LEAST 60 seconds.
                wait = max(60.0, min(float(response.headers.get("Retry-After", "60")), 300.0))
                self.rate_limits += 1
                previous_delay = self.delay
                self.delay = min(15.0, max(self.delay + 0.5, self.delay * 1.5))
                LOGGER.warning("Lichess %s HTTP 429: waiting %.0fs (attempt %d/6); request interval %.1fs -> %.1fs",
                               service, wait, attempt + 1, previous_delay, self.delay)
                time.sleep(wait)
                continue
            if response.status_code in (401, 403):
                raise RuntimeError("Lichess rejected authentication. Set LICHESS_TOKEN "
                                   "(personal token with no scopes, https://lichess.org/account/oauth/token).")
            if response.status_code >= 500:
                LOGGER.warning("Lichess %s HTTP %s", service, response.status_code)
                time.sleep(8 + attempt * 5)
                continue
            response.raise_for_status()
            try:
                data = response.json()
            except ValueError:
                raise RuntimeError(f"Invalid JSON from {service}: {response.text[:180]}")
            path.write_text(json.dumps(data), encoding="utf-8")
            return data
        LOGGER.warning("Skipping %s after multiple failures", service)
        return None


def sample_pgn_games(games: list[chess.pgn.Game], limit: int) -> tuple[list[chess.pgn.Game], int, int]:
    """Keep a deterministic, chapter-balanced sample of full root-to-leaf PGN lines.

    Mainline is sampled first from every chapter, then alternatives are distributed
    round-robin among chapters. Complete move sequences, comments and NAGs survive.
    """
    grouped = []
    total = 0
    for game in games:
        paths: list[tuple[tuple[int, ...], list[chess.pgn.ChildNode]]] = []

        def walk(node: chess.pgn.Game | chess.pgn.ChildNode,
                 path: list[chess.pgn.ChildNode], order: tuple[int, ...]):
            if not node.variations:
                if path:  # An empty chapter has no complete lines.
                    paths.append((order, path))
                return
            for i, child in enumerate(node.variations):
                walk(child, [*path, child], (*order, i))

        walk(game, [], ())
        # Mainline first, then alternatives that diverge sooner: useful to
        # validate distinct branches instead of 16 almost-identical leaves.
        def sample_priority(item):
            indices = item[0]
            first_divergence = next((i for i, n in enumerate(indices) if n), -1)
            return (0 if first_divergence < 0 else 1,
                    first_divergence if first_divergence >= 0 else -1,
                    sum(n != 0 for n in indices), sum(indices), indices)
        paths.sort(key=sample_priority)
        grouped.append((game, [path for _, path in paths]))
        total += len(paths)

    picked: dict[int, list[list[chess.pgn.ChildNode]]] = defaultdict(list)
    indices = [0] * len(grouped)
    remaining = min(limit, total)
    while remaining:
        progress = False
        for chapter, (_, paths) in enumerate(grouped):
            if indices[chapter] < len(paths) and remaining:
                picked[chapter].append(paths[indices[chapter]])
                indices[chapter] += 1
                remaining -= 1
                progress = True
        if not progress:
            break

    output = []
    for chapter, (source, _) in enumerate(grouped):
        if not picked[chapter]:
            continue
        included = {id(child) for path in picked[chapter] for child in path}
        new_game = chess.pgn.Game()
        new_game.headers.clear()
        new_game.headers.update(source.headers)
        new_game.comment = source.comment
        new_game.nags.update(source.nags)

        def copy_tree(src: chess.pgn.Game | chess.pgn.ChildNode,
                      dst: chess.pgn.Game | chess.pgn.ChildNode):
            for child in src.variations:
                if id(child) not in included:
                    continue
                copied = dst.add_variation(child.move)
                copied.comment = child.comment
                copied.starting_comment = child.starting_comment
                copied.nags.update(child.nags)
                copy_tree(child, copied)

        copy_tree(source, new_game)
        output.append(new_game)
    return output, total, sum(len(v) for v in picked.values())


class Repertoire:
    def __init__(self, path: Path, sample_lines: int = 0):
        self.games: list[chess.pgn.Game] = []
        self.nodes: list[dict] = []
        self.roots: list[int] = []
        self.by_key: dict[str, list[int]] = defaultdict(list)
        input_games = []
        with path.open(encoding="utf-8-sig", errors="replace") as fp:
            while (game := chess.pgn.read_game(fp)) is not None:
                if game.errors:
                    LOGGER.warning("PGN parsing errors in %r: %s", game.headers.get("Event"), game.errors[:3])
                if game.headers.get("Variant", "Standard").lower() not in ("standard", "chess"):
                    raise ValueError("Only standard-chess PGNs are supported.")
                input_games.append(game)
        self.original_chapters = len(input_games)
        self.available_lines = 0
        self.sampled_lines = 0
        if sample_lines:
            input_games, self.available_lines, self.sampled_lines = sample_pgn_games(input_games, sample_lines)
        for game in input_games:
                chapter_id = len(self.games)
                self.games.append(game)
                title = game.headers.get("ChapterName") or game.headers.get("Event") or f"Chapter {chapter_id + 1}"

                def add(pgn_node: chess.pgn.Game | chess.pgn.ChildNode, parent: int | None,
                        board: chess.Board, depth: int, san: str, uci: str,
                        variation_index: int) -> int:
                    idx = len(self.nodes)
                    node = {
                        "id": idx, "parent": parent, "chapter": chapter_id,
                        "chapter_name": title, "depth": depth, "san": san, "uci": uci,
                        "mainline": variation_index == 0,
                        "fen": board.fen(), "key": pos_key(board),
                        "turn": "white" if board.turn == chess.WHITE else "black",
                        "comment": pgn_node.comment,
                        "starting_comment": getattr(pgn_node, "starting_comment", ""),
                        "nags": sorted(pgn_node.nags), "children": [],
                        "stats": None, "eval": None, "trap": None,
                        "practical_trap": None, "importance": 0.0,
                        "keep": False,
                    }
                    self.nodes.append(node)
                    self.by_key[node["key"]].append(idx)
                    if parent is not None:
                        self.nodes[parent]["children"].append(idx)
                    for j, child in enumerate(pgn_node.variations):
                        new_board = board.copy(stack=False)
                        move_san = board.san(child.move)
                        new_board.push(child.move)
                        add(child, idx, new_board, depth + 1, move_san, child.move.uci(), j)
                    return idx

                root = add(game, None, game.board(), 0, "", "", 0)
                self.roots.append(root)
        if not self.games:
            raise ValueError("The PGN is empty or contains no valid games.")

    def line(self, idx: int) -> str:
        path = []
        while self.nodes[idx]["parent"] is not None:
            node = self.nodes[idx]
            parent = self.nodes[node["parent"]]
            if parent["depth"] % 2 == 0:
                path.append(f"{(parent['depth'] // 2) + 1}. {node['san']}")
            else:
                path.append(f"{(parent['depth'] // 2) + 1}... {node['san']}")
            idx = node["parent"]
        return " ".join(reversed(path))

    def stats_for_key(self, cache: dict[str, dict | None], side: str = "black"):
        """Positions and edges are distinct: edge counts come from parent's moves array."""
        for node in self.nodes:
            parent_id = node["parent"]
            if parent_id is None:
                node["stats"] = limited_stats(cache.get(node["key"]))
            else:
                parent = self.nodes[parent_id]
                pdata = cache.get(parent["key"]) or {}
                move_data = next((m for m in pdata.get("moves", []) if m.get("uci") == node["uci"]), None)
                # Position data as fallback (could include transpositions).
                node["stats"] = limited_stats(move_data) if move_data is not None else limited_stats(cache.get(node["key"]))
                node["edge_known"] = move_data is not None
            if node["stats"] is not None:
                node["stats"]["score"] = score(node["stats"], side)
                node["stats"]["n"] = n_games(node["stats"])


def limited_stats(obj: dict | None) -> dict | None:
    if obj is None:
        return None
    return {k: int(obj.get(k, 0)) for k in ("white", "draws", "black")}


def fetch_explorer(rep: Repertoire, client: ApiClient, args) -> dict[str, dict | None]:
    cache: dict[str, dict | None] = {}
    # Best-first: prioritize branches by the number of games reaching them.
    heap = []
    counter = 0
    for root in rep.roots:
        heapq.heappush(heap, (-10**15, 0, counter, root))
        counter += 1
    enqueued = set()
    while heap:
        negweight, depth, _, idx = heapq.heappop(heap)
        node = rep.nodes[idx]
        key = node["key"]
        if key not in cache:
            if args.max_queries and len(cache) >= args.max_queries:
                break
            # Leaf stats usually come directly from the parent's Explorer moves array.
            # Avoid a redundant HTTP request when it is known: a leaf has no
            # continuations to fetch, and transpositions keep consistent edge counts.
            parent_id = node["parent"]
            if parent_id is not None and not node["children"]:
                parent = rep.nodes[parent_id]
                parent_data = cache.get(parent["key"]) or {}
                if any(m.get("uci") == node["uci"] for m in parent_data.get("moves", [])):
                    continue
            # Avoid querying very deep ultra-rare lines when requested; no invented stats.
            if depth and args.query_min_games > 0 and -negweight < args.query_min_games:
                continue
            data = client.fetch("explorer", EXPLORER, {
                "variant": "standard", "fen": key, "speeds": args.speeds,
                "ratings": args.ratings, "moves": 50, "topGames": 0, "recentGames": 0,
            })
            cache[key] = data
            if len(cache) % 15 == 0:
                emit_progress("explorer", len(cache), len(rep.by_key),
                              5 + 50 * min(1, len(cache) / max(1, len(rep.by_key))),
                              "Lichess Explorer: collecting statistics")
            if len(cache) % 30 == 0:
                LOGGER.info("Explorer: %s / %s unique positions (network: %s)",
                            len(cache), len(rep.by_key), client.network_calls)
        data = cache.get(key) or {}
        moves = {m["uci"]: n_games(m) for m in data.get("moves", [])}
        for child_id in node["children"]:
            child = rep.nodes[child_id]
            if child_id in enqueued:
                continue
            enqueued.add(child_id)
            weight = moves.get(child["uci"])
            if weight is None:
                weight = 0
            heapq.heappush(heap, (-weight, child["depth"], counter, child_id))
            counter += 1
    rep.stats_for_key(cache, args.side)
    return cache


def discover_engine(engine_arg: str | None) -> str | None:
    if engine_arg:
        path = shutil.which(engine_arg) or engine_arg
        return path if Path(path).is_file() and os.access(path, os.X_OK) else None
    candidates = [shutil.which("stockfish"), "/opt/homebrew/bin/stockfish",
                  "/usr/local/bin/stockfish", "/usr/bin/stockfish"]
    return next((p for p in candidates if p and Path(p).is_file() and os.access(p, os.X_OK)), None)


def preflight_engine(args) -> str | None:
    """Fail before expensive Explorer calls if a required local engine is missing."""
    engine = discover_engine(args.engine)
    if args.eval_source in ("local", "hybrid") and not engine:
        raise RuntimeError("Stockfish not found: install it with 'brew install stockfish', "
                           "or use --eval-source cloud/none, or --engine /path/to/stockfish.")
    if args.eval_source == "auto" and not engine:
        LOGGER.warning("Stockfish not found: auto will use Lichess Cloud; some evaluations may be missing.")
    if engine:
        LOGGER.info("Stockfish available: %s", engine)
    return engine


def save_json_atomic(path: Path, data: dict):
    """Avoid a truncated JSON cache if interrupted while writing."""
    temp = path.with_suffix(path.suffix + ".tmp")
    temp.write_text(json.dumps(data, ensure_ascii=False), encoding="utf-8")
    os.replace(temp, path)


def evaluate(rep: Repertoire, client: ApiClient, args) -> tuple[int, int]:
    if args.eval_source == "none":
        emit_progress("engine", 0, 0, 95, "Engine evaluations disabled")
        return 0, 0
    engine_path = discover_engine(args.engine)
    source = args.eval_source
    if source == "auto":
        source = "local" if engine_path else "cloud"
    if source in ("local", "hybrid") and not engine_path:
        raise RuntimeError("Stockfish was requested but is unavailable. Run: brew install stockfish.")

    # Prefer paired before/after-opponent-move positions for reliable blunder detection.
    edge_candidates = []
    for child in rep.nodes:
        if child["parent"] is None:
            continue
        parent = rep.nodes[child["parent"]]
        if parent["turn"] == args.side:
            continue
        weight = n_games(child["stats"]) or 0
        edge_candidates.append((weight, parent["key"], child["key"]))
    edge_candidates.sort(reverse=True)
    selected: list[str] = []
    seen: set[str] = set()

    def add(key: str):
        if key not in seen:
            selected.append(key)
            seen.add(key)

    # Cover frequent opponent choices first. Evaluate parent, mistake and the author's
    # selected repertoire reply as a *group*, so a trap can actually be confirmed.
    for _, pk, ck in edge_candidates:
        reply_keys = []
        for idx in rep.by_key[ck]:
            reply_keys.extend(rep.nodes[r]["key"] for r in rep.nodes[idx]["children"][:args.repertoire_options])
        group = list(dict.fromkeys([pk, ck, *reply_keys]))
        need = sum(k not in seen for k in group)
        if args.max_evals and len(selected) + need > args.max_evals:
            continue
        for k in group:
            add(k)
    # Use spare budget for additional repertoire replies.
    for _, _, ck in edge_candidates:
        if ck not in seen:
            continue
        for idx in rep.by_key[ck]:
            for reply in rep.nodes[idx]["children"]:
                key = rep.nodes[reply]["key"]
                if args.max_evals and len(selected) >= args.max_evals:
                    break
                add(key)
    # Remaining positions, by importance.
    ranked = sorted(rep.by_key.keys(), key=lambda k: max(
        (n_games(rep.nodes[i]["stats"]) or 0 for i in rep.by_key[k]), default=0), reverse=True)
    for k in ranked:
        if args.max_evals and len(selected) >= args.max_evals:
            break
        add(k)

    emit_progress("engine", 0, len(selected), 55, "Evaluating positions")
    evals: dict[str, dict | None] = {}
    engine = None
    local_cache_path = None
    local_cached = {}
    if source in ("local", "hybrid"):
        import hashlib
        engine_id = hashlib.sha256(str(Path(engine_path).resolve()).encode()).hexdigest()[:12]
        local_cache_path = args.cache / f"stockfish_{engine_id}_depth{args.depth}.json"
        args.cache.mkdir(parents=True, exist_ok=True)
        if local_cache_path.exists():
            local_cached = json.loads(local_cache_path.read_text(encoding="utf-8"))
    try:
        if source in ("local", "hybrid") and any(key not in local_cached for key in selected):
            engine = chess.engine.SimpleEngine.popen_uci(engine_path)
            try:
                engine.configure({"Threads": args.threads, "Hash": args.hash_mb})
            except chess.engine.EngineError:
                LOGGER.warning("Engine doesn't accept Threads/Hash settings; using defaults.")
        cloud_requests = 0
        for i, key in enumerate(selected, 1):
            if key in local_cached and source in ("local", "hybrid"):
                evals[key] = local_cached[key]
                if i % 10 == 0 or i == len(selected):
                    emit_progress("engine", i, len(selected), 55 + 40 * i / max(1, len(selected)),
                                  "Stockfish: using cached evaluations")
                continue
            ev = None
            if source in ("cloud", "hybrid"):
                # Hybrid mode conservatively limits fresh Cloud lookups: the local
                # engine reliably covers misses, avoiding 1,000+ extra HTTP calls.
                can_cloud = source == "cloud" or cloud_requests < args.cloud_limit
                if can_cloud:
                    params = {"fen": key, "multiPv": 1, "variant": "standard"}
                    calls_before = client.network_calls
                    data = client.fetch("cloud", CLOUD, params)
                    cloud_requests += client.network_calls - calls_before
                    pvs = (data.get("pvs") or []) if data else []
                    pv = pvs[0] if pvs else {}
                    if "cp" in pv or "mate" in pv:
                        ev = {"cp": pv["cp"] if "cp" in pv else (100000 if pv["mate"] > 0 else -100000),
                              "mate": pv.get("mate"), "depth": data.get("depth"), "source": "cloud"}
            if ev is None and source in ("local", "hybrid"):
                board = chess.Board(key + " 0 1")
                try:
                    if board.is_game_over(claim_draw=False):
                        if board.is_checkmate():
                            winner_white = not board.turn
                            ev = {"cp": 100000 if winner_white else -100000,
                                  "mate": 0, "depth": 0, "source": "local"}
                        else:
                            ev = {"cp": 0, "depth": 0, "source": "local"}
                    else:
                        info = engine.analyse(board, chess.engine.Limit(depth=args.depth))
                        sc = info["score"].pov(chess.WHITE)
                        ev = {"cp": sc.score(mate_score=100000), "mate": sc.mate(),
                              "depth": info.get("depth", args.depth), "source": "local"}
                except (chess.engine.EngineError, chess.engine.EngineTerminatedError) as exc:
                    LOGGER.warning("Stockfish error for %s: %s", key, exc)
                    ev = None
                local_cached[key] = ev
                if i % 10 == 0 and local_cache_path:
                    save_json_atomic(local_cache_path, local_cached)
            evals[key] = ev
            if i % 10 == 0 or i == len(selected):
                emit_progress("engine", i, len(selected), 55 + 40 * i / max(1, len(selected)),
                              "Stockfish / Cloud analysis")
            if i % 40 == 0:
                LOGGER.info("Engine: %s / %s positions; %s evaluations found", i, len(selected),
                            sum(v is not None for v in evals.values()))
    finally:
        if source in ("local", "hybrid") and local_cache_path:
            save_json_atomic(local_cache_path, local_cached)
        if engine:
            engine.quit()
    for node in rep.nodes:
        node["eval"] = evals.get(node["key"])
    return len(selected), sum(v is not None for v in evals.values())


def detect_traps(rep: Repertoire, args) -> list[dict]:
    traps = []
    sign = 1 if args.side == "white" else -1
    for child in rep.nodes:
        if child["parent"] is None:
            continue
        parent = rep.nodes[child["parent"]]
        if parent["turn"] == args.side:
            continue  # Only opponent mistakes, never errors by the repertoire player.
        before, after = parent["eval"], child["eval"]
        if before is None or after is None:
            continue
        if sign * (after["cp"] - before["cp"]) < args.trap_drop or sign * after["cp"] < args.trap_advantage:
            continue
        replies = []
        for id_ in child["children"][:args.repertoire_options]:
            r = rep.nodes[id_]
            ev = r["eval"]
            replies.append({"id": id_, "san": r["san"],
                            "eval": ev["cp"] if ev else None,
                            "works": ev is not None and sign * ev["cp"] >= args.trap_advantage})
        valid_reply = next((r for r in replies if r["works"]), None)
        playable = bool(valid_reply)
        label = "confirmed" if playable else "unverified"
        trap = {
            "id": child["id"], "parent": parent["id"], "delta": sign * (after["cp"] - before["cp"]),
            "cp": after["cp"], "n": n_games(child["stats"]),
            "score": score(child["stats"], args.side), "san": child["san"],
            "status": label, "confirmed": playable,
            "response": valid_reply["san"] if valid_reply else (replies[0]["san"] if replies else "—"),
        }
        child["trap"] = trap
        traps.append(trap)
    traps.sort(key=lambda t: (t["confirmed"], t["n"] or 0, t["delta"]), reverse=True)
    return traps


def classify_practical_traps(rep: Repertoire, args) -> list[dict]:
    """Practical trap: near-equal position with strong practical results for repertoire side.

    Observational, not causal. Only positions where the opponent is to move
    are considered: our repertoire side has just presented the opponent a choice.
    """
    import math
    traps = []
    for node in rep.nodes:
        ev, s = node["eval"], node["stats"]
        if not ev or not s or node["depth"] < 3 or node["turn"] == args.side:
            continue
        count = n_games(s) or 0
        cp = ev.get("cp")
        if cp is None or abs(cp) > args.practical_max_abs_cp or count < args.practical_min_games:
            continue
        observed = score(s, args.side)
        # Engine is always white-POV; convert to repertoire-player POV.
        signed_cp = cp if args.side == "white" else -cp
        expected = 1.0 / (1.0 + math.exp(max(-50, min(50, -signed_cp / 180.0))))
        # Mild Bayesian shrinkage towards 50% to reduce rare-sample false positives.
        shrunk = ((s[args.side] + s["draws"] * 0.5) + 10.0) / (count + 20.0)
        gap = shrunk - expected
        if observed is None or observed < args.practical_min_score or gap < args.practical_min_gap:
            continue
        trap = {"id": node["id"], "n": count, "score": observed,
                "expected": expected, "gap": gap, "eval_cp": cp,
                "label": "Practical trap / deceptive position"}
        node["practical_trap"] = trap
        traps.append(trap)
    traps.sort(key=lambda x: (x["n"] * x["gap"]), reverse=True)
    return traps


def calculate_importance(rep: Repertoire, args):
    """0..100: frequency 55%, practical results 25%, objective eval 20%.

    Frequency is log-normalized within each chapter. Scores are shrunk toward 0.5
    based on sample size; missing data contribute neutral, never fabricated data.
    A small bonus is awarded to objectively balanced practical traps.
    """
    import math
    chapter_n = defaultdict(int)
    for node in rep.nodes:
        if node["depth"] >= 3:
            chapter_n[node["chapter"]] = max(chapter_n[node["chapter"]], n_games(node["stats"]) or 0)
    for node in rep.nodes:
        s = node["stats"]
        n = n_games(s) or 0
        reference = chapter_n[node["chapter"]]
        freq = math.log1p(n) / math.log1p(reference) if reference else 0.0
        observed = score(s, args.side)
        practical = (observed * n + 0.5 * 40) / (n + 40) if observed is not None else 0.5
        ev = node["eval"]
        objective = (1.0 + math.tanh((1 if args.side == "white" else -1) * ev["cp"] / 240.0)) / 2.0 if ev and ev.get("cp") is not None else 0.5
        bonus = 0.10 if node["practical_trap"] else 0.0
        node["importance"] = round(100 * min(1.0, 0.55 * freq + 0.25 * practical + 0.20 * objective + bonus), 2)


def prune(rep: Repertoire, args):
    if args.filter_mode == "target":
        from line_selector import select_target_lines
        selection = select_target_lines(rep.nodes, rep.roots, args.target_lines, args.min_chapter_lines, args.min_games, args.trap_boost, args.repertoire_options, args.side)
        for node in rep.nodes:
            node["keep"] = node["id"] in selection["kept"]
        LOGGER.info("Target Lines: %d/%d available lines; %d nodes; per chapter: %s",
                    len(selection["leaf_ids"]), selection["available"], len(selection["kept"]), selection["per_chapter"])
        return

    """Repertoire-aware selection, reproducible by the standalone HTML dashboard.

    At repertoire-side-to-move nodes retain the author's first N candidate moves.
    At opponent-to-move nodes prioritize by combined importance until cumulative
    *absolute* Lichess frequency reaches coverage. No unknown line is deleted.
    """
    for node in rep.nodes:
        node["keep"] = False

    def visit(idx: int):
        node = rep.nodes[idx]
        node["keep"] = True
        if not node["children"]:
            return
        if node["turn"] == args.side:
            chosen = node["children"][:max(1, args.repertoire_options)]
        else:
            parent_n = n_games(node["stats"])
            children = [rep.nodes[i] for i in node["children"]]
            known = [c for c in children if c.get("edge_known") and n_games(c["stats"]) is not None]
            unknown = [c for c in children if not c.get("edge_known") or n_games(c["stats"]) is None]
            known.sort(key=lambda c: (c["importance"], n_games(c["stats"]) or 0), reverse=True)
            chosen = []
            covered = 0
            for child in known:
                count = n_games(child["stats"]) or 0
                if chosen and parent_n and covered / parent_n >= args.coverage:
                    break
                if chosen and (count < args.min_games or child["importance"] < args.min_importance):
                    continue
                chosen.append(child["id"])
                covered += count
            chosen += [c["id"] for c in unknown]  # keep unmeasured paths
            if args.keep_traps:
                chosen += [c["id"] for c in children if c["trap"] and c["trap"]["confirmed"]
                           and (c["trap"]["n"] or 0) >= args.trap_min_games]
            chosen = list(dict.fromkeys(chosen))
            if not chosen:
                chosen = [node["children"][0]]
        for child_id in chosen:
            visit(child_id)

    for root in rep.roots:
        visit(root)


def export_pgn(rep: Repertoire, output: Path, args):
    with output.open("w", encoding="utf-8") as fp:
        for game, root_idx in zip(rep.games, rep.roots):
            new_game = chess.pgn.Game()
            new_game.headers.clear()
            for k, v in game.headers.items():
                new_game.headers[k] = v
            new_game.comment = game.comment
            new_game.nags.update(game.nags)
            if game.headers.get("FEN"):
                new_game.setup(game.board())
            def recurse(src: chess.pgn.Game | chess.pgn.ChildNode,
                        dst: chess.pgn.Game | chess.pgn.ChildNode, idx: int):
                # Important: output always uses the original variation order, not query ranking.
                for child_index, child_src in enumerate(src.variations):
                    child_id = rep.nodes[idx]["children"][child_index]
                    node = rep.nodes[child_id]
                    if not node["keep"]:
                        continue
                    dst_child = dst.add_variation(child_src.move)
                    dst_child.comment = child_src.comment
                    dst_child.starting_comment = child_src.starting_comment
                    dst_child.nags.update(child_src.nags)
                    if args.annotate and node["stats"]:
                        s = node["stats"]
                        player_score = f"{100 * s['score']:.1f}%" if s["score"] is not None else "n/a"
                        color_name = "White" if args.side == "white" else "Black"
                        summary = f"Lichess {args.speeds} {args.ratings}: {s['n']} games; {color_name} score {player_score}"
                        if node["eval"]:
                            summary += f"; eval {node['eval']['cp']/100:+.2f} (White POV)"
                        dst_child.comment = (dst_child.comment + "\n" + summary).strip()
                    recurse(child_src, dst_child, child_id)
            recurse(game, new_game, root_idx)
            print(new_game.accept(chess.pgn.StringExporter(headers=True, variations=True, comments=True)),
                  file=fp, end="\n\n")


def report_payload(rep: Repertoire, traps: list[dict], args, stats_unique: int,
                   eval_attempted: int, eval_found: int) -> dict:
    for node in rep.nodes:
        node["line"] = rep.line(node["id"])
    positions = []
    for key, ids in rep.by_key.items():
        candidates = [rep.nodes[i] for i in ids]
        representative = min(candidates, key=lambda n: (n["depth"], n["id"]))
        if representative["depth"] < 3:
            continue
        n = max((n_games(c["stats"]) or 0 for c in candidates), default=0)
        positions.append({"id": representative["id"], "n": n,
                          "score": score(representative["stats"], args.side),
                          "chapter": representative["chapter_name"]})
    positions.sort(key=lambda p: p["n"], reverse=True)
    leaves = [{"id": node["id"], "n": n_games(node["stats"]),
               "score": score(node["stats"], args.side), "kept": node["keep"]}
              for node in rep.nodes if not node["children"]]
    leaves.sort(key=lambda n: (n["n"] or 0), reverse=True)
    return {
        "config": {"side": args.side, "repertoire_options": args.repertoire_options,
                   "filter_mode": args.filter_mode, "target_lines": args.target_lines,
                   "min_chapter_lines": args.min_chapter_lines, "trap_boost": args.trap_boost,
                   "ratings": args.ratings, "speeds": args.speeds, "coverage": args.coverage,
                   "min_games": args.min_games, "black_options": args.repertoire_options,
                   "min_importance": args.min_importance, "keep_traps": args.keep_traps,
                   "trap_min_games": args.trap_min_games, "eval_source": args.eval_source,
                   "dry_run": args.dry_run, "sampled_lines": rep.sampled_lines,
                   "available_lines": rep.available_lines,
                   "created": datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")},
        "summary": {"chapters": len(rep.games), "nodes": len(rep.nodes),
                    "positions": len(rep.by_key), "queried": stats_unique,
                    "eval_attempted": eval_attempted, "eval_found": eval_found,
                    "kept": sum(n["keep"] for n in rep.nodes),
                    "traps": sum(t["confirmed"] for t in traps)},
        "nodes": rep.nodes, "roots": rep.roots,
        "headers": [dict(g.headers) for g in rep.games],
        "positions": positions, "traps": traps, "leaves": leaves,
        "practical_traps": [n["practical_trap"] for n in rep.nodes if n["practical_trap"]],
    }


def write_html(report: dict, output: Path):
    tpl = Path(__file__).with_name("dashboard_template.html").read_text(encoding="utf-8")
    data = json.dumps(report, ensure_ascii=False, separators=(",", ":"))
    data = data.replace("</", "<\\/")  # safe to embed within <script>
    from branding import embed_icon
    html = tpl.replace("/*__EMBED_DATA__*/", "const DATA = " + data + ";")
    output.write_text(embed_icon(html), encoding="utf-8")


def parse_args(argv=None):
    p = argparse.ArgumentParser(description="PGN → Lichess statistics, practical traps and a reduced repertoire PGN.")
    p.add_argument("pgn", type=Path, help="Repertoire PGN, including multi-chapter Lichess studies")
    p.add_argument("--out", type=Path, default=Path("output"), help="Output directory (default: output)")
    p.add_argument("--ratings", default="1800,2000", help="Lichess rating bands (1800,2000 corresponds to 1800–2199)")
    p.add_argument("--speeds", default="blitz,rapid", help="Time controls (default: blitz,rapid)")
    p.add_argument("--side", choices=("black", "white"), default="black",
                   help="Repertoire player side (default: black)")
    p.add_argument("--filter-mode", choices=("target", "coverage"), default="target",
                   help="target = study-line budget; coverage = opponent move coverage")
    p.add_argument("--target-lines", type=int, default=50, help="Number of terminal study lines to keep (default: 50)")
    p.add_argument("--min-chapter-lines", type=int, default=2, help="Minimum study lines per chapter when the budget permits (default: 2)")
    p.add_argument("--trap-boost", type=float, default=1.0, help="Trap-priority multiplier (0–3, default: 1)")
    p.add_argument("--coverage", type=float, default=0.85,
                   help="Opponent move coverage fraction, 0–1 (default: 0.85)")
    p.add_argument("--min-importance", type=float, default=0,
                   help="Minimum importance for secondary variations (0–100; default: 0)")
    p.add_argument("--practical-min-games", type=int, default=40,
                   help="Minimum game sample for practical traps (default: 40)")
    p.add_argument("--practical-min-score", type=float, default=0.66,
                   help="Minimum score for the repertoire side in practical traps (default: 0.66)")
    p.add_argument("--practical-max-abs-cp", type=int, default=60,
                   help="Maximum absolute engine centipawns for practical traps (default: 60)")
    p.add_argument("--practical-min-gap", type=float, default=0.10,
                   help="Minimum shrunken-score versus expected-score gap (default: 0.10)")
    p.add_argument("--min-games", type=int, default=20,
                   help="Skip low-volume secondary branches when covered (default: 20)")
    p.add_argument("--repertoire-options", "--black-options", dest="repertoire_options", type=int, default=1,
                   help="How many repertoire-side alternatives to keep (default: 1)")
    p.add_argument("--keep-traps", action=argparse.BooleanOptionalAction, default=True,
                   help="Preserve confirmed opponent mistakes detected by the engine")
    p.add_argument("--trap-drop", type=int, default=100,
                   help="Minimum opponent mistake, in centipawns (default: 100)")
    p.add_argument("--trap-advantage", type=int, default=80,
                   help="Minimum advantage for the repertoire side, in centipawns (default: 80)")
    p.add_argument("--trap-min-games", type=int, default=10)
    p.add_argument("--eval-source", choices=("auto", "cloud", "local", "hybrid", "none"), default="auto",
                   help="auto: local Stockfish if available, otherwise Cloud; hybrid: Cloud then local fallback")
    p.add_argument("--cloud-limit", type=int, default=50,
                   help="Maximum fresh Cloud calls in hybrid mode (default: 50)")
    p.add_argument("--engine", default=None, help="Path to the Stockfish executable")
    p.add_argument("--depth", type=int, default=14, help="Stockfish search depth (default: 14)")
    p.add_argument("--threads", type=int, default=2, help="Stockfish thread count")
    p.add_argument("--hash-mb", type=int, default=128, help="Hash Stockfish MB")
    p.add_argument("--max-evals", type=int, default=250,
                   help="Maximum positions to evaluate (default: 250; 0 = all; best with local engine)")
    p.add_argument("--max-queries", type=int, default=0,
                   help="Maximum Explorer positions to query (default: 0 = all)")
    p.add_argument("--query-min-games", type=int, default=0,
                   help="Skip deep, low-volume queries (default: 0 = disabled)")
    p.add_argument("--delay", type=float, default=3.0,
                   help="Minimum interval between HTTP calls (default: 3 seconds; adaptive after 429)")
    p.add_argument("--dry-run", action="store_true",
                   help="Analyze a chapter-balanced sample of complete lines")
    p.add_argument("--sample-lines", type=int, default=16,
                   help="Number of complete dry-run lines (default: 16)")
    p.add_argument("--offline", action="store_true",
                   help="Use cached data only, with no network access or fabricated statistics")
    p.add_argument("--cache", type=Path, default=Path.home() / ".cache" / "repertoire_optimizer",
                   help="Persistent API cache directory")
    p.add_argument("--annotate", action="store_true", help="Append game counts and evaluations to PGN comments")
    p.add_argument("--verbose", action="store_true")
    args = p.parse_args(argv)
    if not (0 < args.coverage <= 1):
        p.error("--coverage must be in (0,1].")
    if not (0 <= args.min_importance <= 100 and 0 <= args.practical_min_score <= 1
            and 0 <= args.practical_min_gap <= 1 and args.practical_min_games >= 0
            and args.practical_max_abs_cp >= 0):
        p.error("Invalid importance/practical-trap parameters.")
    if (args.max_evals < 0 or args.max_queries < 0 or args.min_games < 0
            or args.repertoire_options < 1 or args.sample_lines < 1 or args.cloud_limit < 0
            or args.delay < 0):
        p.error("Invalid numeric parameters.")
    if args.target_lines < 1 or args.min_chapter_lines < 0 or not 0 <= args.trap_boost <= 3:
        p.error("--target-lines >= 1, --min-chapter-lines >= 0, --trap-boost between 0 and 3.")
    if not args.pgn.is_file():
        p.error(f"PGN not found: {args.pgn}")
    allowed_ratings = {"0", "1000", "1200", "1400", "1600", "1800", "2000", "2200", "2500"}
    if not set(args.ratings.split(",")) <= allowed_ratings:
        p.error("--ratings contains invalid rating bands.")
    return args


def main(argv=None):
    args = parse_args(argv)
    logging.basicConfig(level=logging.DEBUG if args.verbose else logging.INFO,
                        format="%(asctime)s %(levelname)s %(message)s", datefmt="%H:%M:%S")
    if args.dry_run and args.out == Path("output"):
        args.out = Path("output_dry_run")
    args.out.mkdir(parents=True, exist_ok=True)
    preflight_engine(args)
    rep = Repertoire(args.pgn, sample_lines=args.sample_lines if args.dry_run else 0)
    emit_progress("explorer", 0, len(rep.by_key), 5, "PGN loaded; starting Lichess statistics")
    if args.dry_run:
        LOGGER.info("DRY RUN: %d complete lines selected out of %d; %d/%d chapters",
                    rep.sampled_lines, rep.available_lines, len(rep.games), rep.original_chapters)
    LOGGER.info("PGN: %s chapters; %s nodes; %s distinct positions",
                len(rep.games), len(rep.nodes), len(rep.by_key))
    token = os.environ.get("LICHESS_TOKEN", "").strip()
    if not token and not args.offline:
        LOGGER.warning("LICHESS_TOKEN is not set; uncached requests may return HTTP 401. "
                       "Use --offline to rely exclusively on the local cache.")
    client = ApiClient(args.cache, token, args.delay, args.offline)
    raw = fetch_explorer(rep, client, args)
    queried_direct = sum(x is not None for x in raw.values())
    queried = sum(any(rep.nodes[i]["stats"] is not None for i in ids)
                  for ids in rep.by_key.values())
    LOGGER.info("Explorer: data available for %d/%d positions (direct responses: %d; HTTP calls: %d; rate limits: %d)",
                queried, len(rep.by_key), queried_direct, client.network_calls, client.rate_limits)
    emit_progress("explorer", queried, len(rep.by_key), 55, "Lichess statistics complete")
    attempted, found = evaluate(rep, client, args)
    emit_progress("finalize", 0, 1, 95, "Selecting lines and generating report")
    traps = detect_traps(rep, args)
    practical_traps = classify_practical_traps(rep, args)
    calculate_importance(rep, args)
    LOGGER.info("Traps: %d tactical, %d practical (approximately equal eval, high repertoire-side score)",
                len(traps), len(practical_traps))
    prune(rep, args)
    export_pgn(rep, args.out / "filtered_repertoire.pgn", args)
    report = report_payload(rep, traps, args, queried, attempted, found)
    write_html(report, args.out / "dashboard.html")
    (args.out / "data.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    emit_progress("finalize", 1, 1, 99, "Dashboard and PGN ready")
    LOGGER.info("Ready: %s and %s | kept %s/%s nodes | %s confirmed traps",
                args.out / "dashboard.html", args.out / "filtered_repertoire.pgn",
                report["summary"]["kept"], len(rep.nodes), report["summary"]["traps"])


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, requests.RequestException) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        sys.exit(1)
