"""Embed ChessPrune branding in standalone report HTML (standard library only)."""

import base64
from pathlib import Path


def embed_icon(html: str) -> str:
    """Return self-contained HTML, without any server- or disk-relative image URLs."""
    icon = Path(__file__).resolve().parent / "assets" / "ChessPrune_Icon.png"
    encoded = base64.b64encode(icon.read_bytes()).decode("ascii")
    return html.replace("__CHESSPRUNE_ICON__", "data:image/png;base64," + encoded)
