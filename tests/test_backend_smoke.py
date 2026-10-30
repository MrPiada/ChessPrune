"""Smoke tests for local HTTP delivery and English backend validation.

Run: python3 -m unittest discover -s tests -v
No PGN download, engine, network service, or third-party test runner required.
"""
import json
import sys
import threading
import unittest
import urllib.request
from http.server import ThreadingHTTPServer
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import app


class LocalServerTests(unittest.TestCase):
    def test_english_validation(self):
        with self.assertRaisesRegex(app.InputError, 'Select at least one valid'):
            app.config({'ratings': ['invalid']})
        with self.assertRaisesRegex(app.InputError, 'selected file must have a \\.pgn extension'):
            app.prepare_job({'pgn': {'source': 'upload', 'name': 'invalid.txt',
                                     'text': '[Event "Test"]\n\n*'}})

    def test_http_health_and_frontend(self):
        server = ThreadingHTTPServer(('127.0.0.1', 0), app.Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            url = f'http://127.0.0.1:{server.server_port}'
            client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            health = json.loads(client.open(url + '/api/health', timeout=5).read())
            self.assertIn('chess_installed', health)
            html = client.open(url + '/', timeout=5).read().decode('utf-8')
            self.assertIn('ChessPrune', html)
            self.assertIn('/assets/ChessPrune_Icon.png', html)
            icon = client.open(url + '/assets/ChessPrune_Icon.png', timeout=5).read()
            self.assertTrue(icon.startswith(b'\x89PNG\r\n\x1a\n'))
            self.assertIn('Backend logs', html)
            self.assertIn('Dependencies: checking', html)
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=3)


if __name__ == '__main__':
    unittest.main()
