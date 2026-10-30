#!/usr/bin/env python3
"""Local, loopback-only HTML front end for ChessPrune.

No web-framework dependency. Chess analysis stays in repertoire_optimizer.py.
Launch with: python3 app.py
"""
from __future__ import annotations

import importlib.util
import json
import os
import re
import secrets
import shutil
import signal
import subprocess
import sys
import threading
import time
import webbrowser
from datetime import datetime
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parent
JOBS = ROOT / "analyses"
JOBS.mkdir(exist_ok=True)
STATIC = ROOT / "web" / "index.html"
BRAND_ICON = ROOT / "assets" / "ChessPrune_Icon.png"
BODY_LIMIT = 15 * 1024 * 1024
_active_lock = threading.Lock()
_active: dict = {"id": None, "process": None}

class InputError(Exception):
    pass


def int_field(data, key, default, low, high):
    try:
        v = int(data.get(key, default))
    except (TypeError, ValueError):
        raise InputError(f"{key}: an integer is required")
    if v < low or v > high:
        raise InputError(f"{key}: allowed range {low}–{high}")
    return v


def float_field(data, key, default, low, high):
    try:
        v = float(data.get(key, default))
    except (TypeError, ValueError):
        raise InputError(f"{key}: a number is required")
    if not low <= v <= high or v != v:
        raise InputError(f"{key}: allowed range {low}–{high}")
    return v


def option(data, key, allowed, default):
    v = str(data.get(key, default))
    if v not in allowed:
        raise InputError(f"{key}: invalid value")
    return v


def config(data):
    side = option(data, "side", {"white", "black"}, "black")
    mode = option(data, "mode", {"test", "full"}, "test")
    ratings = data.get("ratings", ["1800", "2000"])
    speeds = data.get("speeds", ["blitz", "rapid"])
    allowed_ratings = {"0", "1000", "1200", "1400", "1600", "1800", "2000", "2200", "2500"}
    allowed_speeds = {"bullet", "blitz", "rapid", "classical", "correspondence"}
    if not isinstance(ratings, list) or not ratings or not set(ratings) <= allowed_ratings:
        raise InputError("Select at least one valid Lichess rating band")
    if not isinstance(speeds, list) or not speeds or not set(speeds) <= allowed_speeds:
        raise InputError("Select at least one valid time control")
    return {
        "side": side,
        "mode": mode,
        "ratings": sorted(set(ratings), key=int),
        "speeds": [v for v in ("bullet", "blitz", "rapid", "classical", "correspondence") if v in speeds],
        "filter_mode": option(data, "filter_mode", {"target", "coverage"}, "target"),
        "eval_source": option(data, "eval_source", {"local", "cloud", "hybrid", "auto", "none"}, "auto"),
        "target_lines": int_field(data, "target_lines", 50, 1, 3000),
        "min_chapter_lines": int_field(data, "min_chapter_lines", 2, 0, 100),
        "repertoire_options": int_field(data, "repertoire_options", 1, 1, 10),
        "min_games": int_field(data, "min_games", 20, 0, 1000000),
        "max_evals": int_field(data, "max_evals", 80 if mode == "test" else 0, 0, 100000),
        "sample_lines": int_field(data, "sample_lines", 16, 1, 3000),
        "depth": int_field(data, "depth", 14, 1, 40),
        "threads": int_field(data, "threads", 2, 1, 64),
        "hash_mb": int_field(data, "hash_mb", 128, 16, 16384),
        "cloud_limit": int_field(data, "cloud_limit", 50, 0, 5000),
        "coverage": float_field(data, "coverage", 0.7, 0.01, 1.0),
        "trap_boost": float_field(data, "trap_boost", 1, 0, 3),
        "min_importance": float_field(data, "min_importance", 0, 0, 100),
        "delay": float_field(data, "delay", 4.0, 0.5, 120),
        "offline": data.get("offline") is True,
        "annotate": data.get("annotate") is True,
        "keep_traps": data.get("keep_traps", True) is True,
        "engine": str(data.get("engine", "")).strip(),
    }


def discover_engine(engine=""):
    paths = [engine] if engine else [shutil.which("stockfish"), "/opt/homebrew/bin/stockfish",
                                     "/usr/local/bin/stockfish", "/usr/bin/stockfish"]
    for path in paths:
        if path:
            p = shutil.which(path) or path
            if Path(p).is_file() and os.access(p, os.X_OK):
                return str(p)
    return None


def job_path(job_id):
    if not re.fullmatch(r"[a-zA-Z0-9_-]{4,72}", job_id):
        raise InputError("Invalid analysis identifier")
    p = JOBS / job_id
    if not p.is_dir():
        raise FileNotFoundError(job_id)
    return p


def read_meta(job_id):
    p = job_path(job_id)
    return json.loads((p / "meta.json").read_text(encoding="utf-8"))


def save_meta(p, meta):
    tmp = p / "meta.json.tmp"
    tmp.write_text(json.dumps(meta, ensure_ascii=False, indent=2), encoding="utf-8")
    tmp.replace(p / "meta.json")


def job_public(job_id, include_log=False):
    p = job_path(job_id)
    meta = read_meta(job_id)
    with _active_lock:
        active_here = _active["id"] == job_id
    if meta.get("status") == "running" and not active_here:
        meta["status"] = "interrupted"
    out = p / "results"
    files = {name: f"/files/{job_id}/{name}" for name in
             ("dashboard.html", "filtered_repertoire.pgn", "data.json") if (out / name).is_file()}
    # Any configured target, not only the default 50 lines.
    targets = sorted(out.glob("target_repertoire_*_lines.pgn"), key=lambda f: f.stat().st_mtime, reverse=True)
    if targets:
        files["target_pgn"] = f"/files/{job_id}/{targets[0].name}"
    meta = {**meta, "files": files}
    if include_log:
        log = p / "console.log"
        if log.exists():
            # Only return last 48KB of logs, even for enormous analyses.
            with log.open("rb") as fp:
                fp.seek(max(0, log.stat().st_size - 48_000))
                meta["log"] = fp.read().decode("utf-8", errors="replace")
        else:
            meta["log"] = ""
    return meta


def list_jobs():
    out = []
    for p in JOBS.iterdir():
        if p.is_dir() and (p / "meta.json").is_file():
            try:
                out.append(job_public(p.name))
            except (OSError, ValueError):
                continue
    return sorted(out, key=lambda a: a.get("created", ""), reverse=True)[:75]


def build_argv(cfg, pgn_file, out):
    args = [sys.executable, "-u", str(ROOT / "repertoire_optimizer.py"), str(pgn_file),
            "--out", str(out),
            "--side", cfg["side"], "--ratings", ",".join(cfg["ratings"]),
            "--speeds", ",".join(cfg["speeds"]),
            "--filter-mode", cfg["filter_mode"],
            "--eval-source", cfg["eval_source"],
            "--target-lines", str(cfg["target_lines"]),
            "--min-chapter-lines", str(cfg["min_chapter_lines"]),
            "--repertoire-options", str(cfg["repertoire_options"]),
            "--min-games", str(cfg["min_games"]),
            "--max-evals", str(cfg["max_evals"]),
            "--depth", str(cfg["depth"]),
            "--threads", str(cfg["threads"]),
            "--hash-mb", str(cfg["hash_mb"]),
            "--cloud-limit", str(cfg["cloud_limit"]),
            "--coverage", str(cfg["coverage"]),
            "--min-importance", str(cfg["min_importance"]),
            "--trap-boost", str(cfg["trap_boost"]),
            "--delay", str(cfg["delay"])]
    if cfg["mode"] == "test":
        args += ["--dry-run", "--sample-lines", str(cfg["sample_lines"])]
    if cfg["offline"]:
        args.append("--offline")
    if cfg["annotate"]:
        args.append("--annotate")
    if not cfg["keep_traps"]:
        args.append("--no-keep-traps")
    if cfg["engine"]:
        args += ["--engine", cfg["engine"]]
    return args


def run_job(jid, argv, token):
    p = job_path(jid)
    meta = read_meta(jid)
    env = dict(os.environ)
    if token:
        env["LICHESS_TOKEN"] = token
    env["PYTHONUNBUFFERED"] = "1"
    proc = None
    try:
        with (p / "console.log").open("a", encoding="utf-8") as log:
            log.write("Starting PGN analysis; shared Lichess cache enabled.\n")
            log.flush()
            proc = subprocess.Popen(argv, cwd=ROOT, stdout=subprocess.PIPE,
                                    stderr=subprocess.STDOUT, text=True, encoding="utf-8",
                                    errors="replace", bufsize=1, env=env, start_new_session=True)
            with _active_lock:
                _active["process"] = proc
                _active["id"] = jid
            meta = read_meta(jid)
            if meta.get("cancel_requested") and proc.poll() is None:
                try:
                    os.killpg(proc.pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
            meta["status"] = "running"
            save_meta(p, meta)
            for line in proc.stdout:
                if line.startswith("@@PROGRESS "):
                    try:
                        update = json.loads(line[len("@@PROGRESS "):])
                        percent = float(update["percent"])
                        if 0 <= percent <= 100 and update.get("phase") in {"explorer", "engine", "finalize"}:
            # Progress is cumulative and does not reset at phase boundaries.
                            meta = read_meta(jid)
                            meta["progress"] = {"phase": update["phase"], "percent": round(percent, 1),
                                                "done": max(0, int(update.get("done", 0))),
                                                "total": max(0, int(update.get("total", 0))),
                                                "message": str(update.get("message", ""))[:200]}
                            save_meta(p, meta)
                            continue
                    except (ValueError, TypeError, KeyError):
                        pass
                log.write(line)
                log.flush()
            rc = proc.wait()
            meta = read_meta(jid)
            # Produce a ready-to-use target PGN from the successful analysis.
            # No HTTP requests and no Stockfish: reconstruction uses data.json.
            if rc == 0 and meta["config"]["filter_mode"] == "target":
                cfg = meta["config"]
                rebuild = [sys.executable, "-u", str(ROOT / "rebuild_dashboard.py"),
                           str(p / "results" / "data.json"), "--out",
                           str(p / "results" / "dashboard.html"),
                           "--filter-mode", "target", "--target-lines", str(cfg["target_lines"]),
                           "--min-chapter-lines", str(cfg["min_chapter_lines"]),
                           "--trap-boost", str(cfg["trap_boost"])]
                log.write("\nBuilding Target Lines PGN and dashboard...\n")
                log.flush()
                meta = read_meta(jid)
                meta["progress"] = {"phase": "finalize", "percent": 97, "done": 0, "total": 1,
                                    "message": "Creating dashboard and Target Lines PGN"}
                save_meta(p, meta)
                result = subprocess.run(rebuild, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT)
                if result.returncode:
                    rc = result.returncode
            meta["return_code"] = rc
            meta["status"] = "completed" if rc == 0 else ("cancelled" if meta.get("cancel_requested") else "error")
            if rc == 0:
                meta["progress"] = {"phase": "done", "percent": 100, "done": 1, "total": 1,
                                    "message": "Analysis completed"}
            meta["ended"] = datetime.now().isoformat(timespec="seconds")
            save_meta(p, meta)
    except Exception as exc:
        meta = read_meta(jid)
        meta["status"] = "error"
        meta["error"] = str(exc)
        save_meta(p, meta)
        with (p / "console.log").open("a", encoding="utf-8") as log:
            log.write(f"BACKEND ERROR: {exc}\n")
    finally:
        with _active_lock:
            if _active["id"] == jid:
                _active["process"] = None
                _active["id"] = None


def prepare_job(payload):
    cfg = config(payload)
    token = str(payload.get("token") or "").strip()
    # Secret is passed through environment and never written to report or disk.
    upload = payload.get("pgn", {})
    if not isinstance(upload, dict):
        raise InputError("Invalid PGN")
    if upload.get("source") != "upload":
        raise InputError("Choose a PGN file from your computer")
    content = upload.get("text")
    if not isinstance(content, str) or len(content.encode("utf-8")) > 7 * 1024 * 1024:
        raise InputError("Choose a valid PGN (7 MB maximum)")
    original_name = Path(str(upload.get("name", "repertoire.pgn"))).name
    if not original_name.lower().endswith(".pgn"):
        raise InputError("The selected file must have a .pgn extension")
    if not content.strip() or "[" not in content and not re.search(r"\b\d+\.", content):
        raise InputError("The PGN appears empty or invalid")
    if importlib.util.find_spec("chess") is None or importlib.util.find_spec("requests") is None:
        raise InputError("Missing Python dependencies. Run: python3 -m pip install -r requirements.txt")
    if cfg["eval_source"] in {"local", "hybrid"} and not discover_engine(cfg["engine"]):
        raise InputError("Stockfish not found. Install it with 'brew install stockfish' or select Cloud/None.")
    jid = datetime.now().strftime("%Y%m%d_%H%M%S") + "_" + secrets.token_hex(3)
    p = JOBS / jid
    p.mkdir(parents=True)
    inp = p / "input_repertoire.pgn"
    inp.write_text(content, encoding="utf-8")
    out = p / "results"
    out.mkdir()
    meta = {"id": jid, "name": original_name, "kind": "analysis", "status": "queued", "progress": {"phase": "queued", "percent": 0, "done": 0, "total": 0, "message": "Waiting to start"},
            "created": datetime.now().isoformat(timespec="seconds"), "config": cfg}
    save_meta(p, meta)
    argv = build_argv(cfg, inp, out)
    # Reserve the active slot before starting the worker to prevent simultaneous jobs.
    _active["id"] = jid
    th = threading.Thread(target=run_job, args=(jid, argv, token), daemon=True)
    th.start()
    return jid


def import_report(payload):
    report = payload.get("report")
    if not isinstance(report, dict) or not all(k in report for k in ("config", "nodes", "roots", "headers", "summary")):
        raise InputError("This JSON is not a complete analyzer report")
    if len(json.dumps(report)) > 9_000_000:
        raise InputError("The report is too large")
    if not isinstance(report["config"], dict) or not isinstance(report["nodes"], list) or not isinstance(report["roots"], list):
        raise InputError("The report structure is invalid")
    side = report["config"].get("side", "black")
    if side not in {"white", "black"}:
        raise InputError("The report specifies an invalid repertoire side")
    jid = datetime.now().strftime("%Y%m%d_%H%M%S") + "_" + secrets.token_hex(3)
    p = JOBS / jid
    (p / "results").mkdir(parents=True)
    (p / "results" / "data.json").write_text(json.dumps(report, ensure_ascii=False), encoding="utf-8")
    meta = {"id": jid, "name": Path(str(payload.get("name", "data.json"))).name[:180], "kind": "import",
            "status": "running", "created": datetime.now().isoformat(timespec="seconds"),
            "config": {"side": side, "filter_mode": report["config"].get("filter_mode", "target")}}
    save_meta(p, meta)
    # Rebuild without requiring chess or Lichess access.
    argv = [sys.executable, "-u", str(ROOT / "rebuild_dashboard.py"), str(p / "results" / "data.json"),
            "--out", str(p / "results" / "dashboard.html"),
            "--filter-mode", "target", "--target-lines", str(min(int(report["config"].get("target_lines", 50)), max(1, len([n for n in report["nodes"] if not n.get("children")]))))]
    with (p / "console.log").open("w", encoding="utf-8") as fp:
        res = subprocess.run(argv, cwd=ROOT, stdout=fp, stderr=subprocess.STDOUT, check=False)
    meta["status"] = "completed" if res.returncode == 0 else "error"
    meta["progress"] = {"phase": "done", "percent": 100 if res.returncode == 0 else 0,
                         "done": 1 if res.returncode == 0 else 0, "total": 1,
                         "message": "Report imported" if res.returncode == 0 else "Import failed"}
    meta["return_code"] = res.returncode
    save_meta(p, meta)
    return jid


class Handler(BaseHTTPRequestHandler):
    server_version = "ChessPrune/1.0"

    def log_message(self, fmt, *args):
        # Do not log uploads, auth tokens, or huge bodies.
        sys.stderr.write("[Web] " + fmt % args + "\n")

    def send_bytes(self, data: bytes, mime="application/octet-stream", status=200, attachment=None):
        self.send_response(status)
        self.send_header("Content-Type", mime)
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("X-Frame-Options", "SAMEORIGIN")
        if attachment:
            self.send_header("Content-Disposition", f'attachment; filename="{attachment}"')
        self.end_headers()
        try:
            self.wfile.write(data)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def send_json(self, item, status=200):
        self.send_bytes(json.dumps(item, ensure_ascii=False).encode("utf-8"), "application/json; charset=utf-8", status)

    def read_json(self):
        size = int(self.headers.get("Content-Length", "0"))
        if size <= 0 or size > BODY_LIMIT:
            raise InputError("Invalid request size")
        try:
            obj = json.loads(self.rfile.read(size))
        except (UnicodeError, ValueError) as exc:
            raise InputError(f"Invalid JSON: {exc}")
        if not isinstance(obj, dict):
            raise InputError("Invalid JSON")
        return obj

    def authorized(self, changing=False):
        # Blocks DNS rebinding / cross-origin browser requests to localhost.
        host = self.headers.get("Host", "")
        if not re.fullmatch(r"(?:127\.0\.0\.1|localhost)(?::[0-9]{1,5})?", host):
            self.send_json({"error": "Unauthorized host"}, 403)
            return False
        if changing:
            origin = self.headers.get("Origin", "")
            if origin and origin not in (f"http://{host}",):
                self.send_json({"error": "Unauthorized origin"}, 403)
                return False
            if self.headers.get("Content-Type", "").split(";")[0].lower() != "application/json":
                self.send_json({"error": "JSON request required"}, 415)
                return False
        return True

    def do_GET(self):
        if not self.authorized():
            return
        url = urlparse(self.path)
        path = url.path
        try:
            if path in ("/", "/index.html"):
                self.send_bytes(STATIC.read_bytes(), "text/html; charset=utf-8")
            elif path == "/assets/ChessPrune_Icon.png":
                self.send_bytes(BRAND_ICON.read_bytes(), "image/png")
            elif path == "/api/health":
                self.send_json({"engine": discover_engine(), "python": sys.version.split()[0],
                                "chess_installed": importlib.util.find_spec("chess") is not None,
                                "requests_installed": importlib.util.find_spec("requests") is not None})
            elif path == "/api/jobs":
                self.send_json({"jobs": list_jobs()})
            elif path.startswith("/api/jobs/"):
                jid = path.removeprefix("/api/jobs/")
                self.send_json(job_public(jid, include_log=True))
            elif path.startswith("/files/"):
                bits = path.split("/")
                if len(bits) != 4:
                    raise FileNotFoundError(path)
                _, _, jid, name = bits
                allow = {"dashboard.html", "data.json", "filtered_repertoire.pgn"}
                if name not in allow and not re.fullmatch(r"target_repertoire_[0-9]+_lines\.pgn", name):
                    raise FileNotFoundError(name)
                file = job_path(jid) / "results" / name
                if not file.is_file():
                    raise FileNotFoundError(name)
                mime = ("text/html; charset=utf-8" if name.endswith(".html") else
                        "application/json; charset=utf-8" if name.endswith(".json") else
                        "application/x-chess-pgn; charset=utf-8")
                self.send_bytes(file.read_bytes(), mime,
                                attachment=name if not name.endswith(".html") else None)
            else:
                self.send_json({"error": "Page not found"}, 404)
        except (FileNotFoundError, InputError):
            self.send_json({"error": "Resource not found"}, 404)
        except Exception as exc:
            self.send_json({"error": f"Server error: {exc}"}, 500)

    def do_POST(self):
        if not self.authorized(changing=True):
            return
        path = urlparse(self.path).path
        try:
            data = self.read_json()
            if path == "/api/jobs":
                # One active analysis at a time to protect RAM, CPU and Lichess rate limits.
                with _active_lock:
                    if _active["id"] is not None:
                        raise InputError("Another analysis is already running")
                    jid = prepare_job(data)
                self.send_json({"id": jid, "job": job_public(jid)})
            elif path == "/api/import":
                self.send_json({"id": import_report(data)})
            elif path.startswith("/api/cancel/"):
                jid = path.removeprefix("/api/cancel/")
                with _active_lock:
                    if _active["id"] != jid:
                        raise InputError("This analysis is not currently running")
                    proc = _active["process"]
                meta = read_meta(jid)
                meta["cancel_requested"] = True
                save_meta(job_path(jid), meta)
                if proc and proc.poll() is None:
                    try:
                        os.killpg(proc.pid, signal.SIGTERM)
                    except ProcessLookupError:
                        pass
                self.send_json({"ok": True})
            else:
                self.send_json({"error": "Route not found"}, 404)
        except InputError as exc:
            self.send_json({"error": str(exc)}, 400)
        except Exception as exc:
            self.send_json({"error": str(exc)}, 500)


def main():
    import argparse
    parser = argparse.ArgumentParser(description="Start the local ChessPrune web interface")
    parser.add_argument("--port", type=int, default=8765)
    parser.add_argument("--no-browser", action="store_true")
    args = parser.parse_args()
    host = "127.0.0.1"  # never accessible from the LAN / Internet
    port = args.port
    for _ in range(30):
        try:
            srv = ThreadingHTTPServer((host, port), Handler)
            break
        except OSError:
            port += 1
    else:
        parser.error("Could not bind to an available local port")
    url = f"http://{host}:{port}/"
    print(f"\nChessPrune  |  {url}\nLocal access only. Press Ctrl+C to stop the server.\n", flush=True)
    if not args.no_browser:
        threading.Timer(0.6, lambda: webbrowser.open(url)).start()
    try:
        srv.serve_forever(poll_interval=0.2)
    except KeyboardInterrupt:
        print("Server stopped.")
    finally:
        srv.server_close()
        with _active_lock:
            proc = _active.get("process")
        if proc is not None and proc.poll() is None:
            try:
                os.killpg(proc.pid, signal.SIGTERM)
                print("Active analysis interrupted when the server stopped.")
            except ProcessLookupError:
                pass

if __name__ == "__main__":
    main()
