"""Tests for the four vanDyke TRMNL transforms.

Run with: python3 trmnl/tests/test_vandyke_transform.py
"""

import importlib.util
import json
import os
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.join(HERE, "..", "plugins")


def _load(slug):
    path = os.path.join(ROOT, slug, "src", "transform.py")
    spec = importlib.util.spec_from_file_location(slug.replace("-", "_") + "_transform", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


stats_mod = _load("vandyke-stats")
latest_mod = _load("vandyke-latest")
photo_mod = _load("vandyke-photo")
dashboard_mod = _load("vandyke-dashboard")


class _Resp:
    def __init__(self, payload):
        self._data = json.dumps(payload).encode("utf-8")

    def __enter__(self):
        return self

    def __exit__(self, *args):
        return False

    def read(self):
        return self._data


STATS = {
    "total_entries": 105,
    "unique_words": 87,
    "first_entry_at": "2017-10-02T00:00:00Z",
    "last_entry_at": "2026-03-13T22:07:21Z",
    "entries_last_30_days": 4,
}

ENTRIES = {
    "items": [
        {
            "id": 105,
            "word": "feutenreus",
            "meaning": "feature",
            "created_at": "2026-03-13T22:07:21Z",
            "photo_url": "/api/v1/entries/105/photo",
        },
        {
            "id": 104,
            "word": "",
            "meaning": "",
            "created_at": "2026-03-12T10:00:00Z",
            "photo_url": "/api/v1/entries/104/photo",
        },
        {
            "id": 103,
            "word": "kast systeem",
            "meaning": "kassasysteem",
            "created_at": "2017-10-02T00:00:00Z",
        },
    ],
    "total": 105,
    "page": 1,
    "per_page": 25,
    "pages": 5,
}


def _url_of(url):
    return getattr(url, "full_url", str(url))


def _fake(url, timeout=None):
    target = _url_of(url)
    if "/api/v1/stats" in target:
        return _Resp(STATS)
    if "/api/v1/entries" in target:
        return _Resp(ENTRIES)
    raise AssertionError("unexpected URL " + target)


def _input(url="https://vandyke.example.com"):
    return {
        "trmnl": {
            "plugin_settings": {
                "custom_fields_values": {
                    "url": url,
                }
            }
        }
    }


class StatsTest(unittest.TestCase):
    @mock.patch("urllib.request.urlopen", _fake)
    def test_stats(self):
        out = stats_mod.run(_input())
        self.assertEqual(out["total"], 105)
        self.assertEqual(out["unique"], 87)
        self.assertEqual(out["last30"], 4)
        self.assertEqual(out["last_date"], "13 Mar 2026")
        self.assertEqual(out["span_years"], "10")

    def test_missing_url(self):
        out = stats_mod.run(_input(""))
        self.assertIn("error", out)

    @mock.patch("urllib.request.urlopen", side_effect=OSError("down"))
    def test_unreachable(self, *_):
        out = stats_mod.run(_input())
        self.assertIn("error", out)


class LatestTest(unittest.TestCase):
    @mock.patch("urllib.request.urlopen", _fake)
    def test_latest(self):
        out = latest_mod.run(_input())
        self.assertEqual(out["total"], 105)
        self.assertEqual(len(out["entries"]), 3)
        self.assertEqual(out["entries"][0]["word"], "feutenreus")
        self.assertEqual(
            out["entries"][0]["photo_url"],
            "https://vandyke.example.com/api/v1/entries/105/photo",
        )

    def test_missing_url(self):
        self.assertIn("error", latest_mod.run(_input("")))


class PhotoTest(unittest.TestCase):
    @mock.patch("urllib.request.urlopen", _fake)
    def test_photo(self):
        out = photo_mod.run(_input())
        self.assertTrue(out["has_photo"])
        self.assertEqual(out["photo_url"], "https://vandyke.example.com/api/v1/entries/105/photo")
        self.assertEqual(out["word"], "feutenreus")

    @mock.patch("urllib.request.urlopen", _fake)
    def test_no_photo(self):
        payload = {
            "items": [{"id": 1, "word": "a", "meaning": "b", "created_at": "2026-01-01T00:00:00Z"}],
            "total": 1,
        }
        with mock.patch("urllib.request.urlopen", lambda *a, **k: _Resp(payload)):
            out = photo_mod.run(_input())
        self.assertFalse(out["has_photo"])
        self.assertIn("message", out)


class DashboardTest(unittest.TestCase):
    @mock.patch("urllib.request.urlopen", _fake)
    def test_dashboard(self):
        out = dashboard_mod.run(_input())
        self.assertEqual(out["total"], 105)
        self.assertEqual(len(out["entries"]), 3)
        self.assertTrue(out["has_photo"])
        self.assertEqual(out["photo"]["word"], "feutenreus")

    def test_missing_url(self):
        self.assertIn("error", dashboard_mod.run(_input("")))


if __name__ == "__main__":
    unittest.main()
