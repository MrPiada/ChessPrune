"""Regression checks for embedded offline ChessPrune artwork."""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from branding import embed_icon


class BrandingTests(unittest.TestCase):
    def test_offline_dashboard_has_embedded_icon(self):
        html = embed_icon('<img src="__CHESSPRUNE_ICON__">')
        self.assertIn('data:image/png;base64,', html)
        self.assertNotIn('__CHESSPRUNE_ICON__', html)

    def test_assets_included(self):
        assets = Path(__file__).resolve().parents[1] / 'assets'
        self.assertTrue((assets / 'ChessPrune_Icon.png').is_file())
        self.assertTrue((assets / 'ChessPrune_SmarterPreparation.png').is_file())


if __name__ == '__main__':
    unittest.main()
