#!/usr/bin/env python3
"""Compact vanDyke dashboard transform for TRMNL: stats, latest entries, photo."""

import json
import sys
import urllib.request
from datetime import datetime


def _fields(input):
    try:
        return input["trmnl"]["plugin_settings"]["custom_fields_values"]
    except (KeyError, TypeError):
        return {}


def _base(input):
    url = (_fields(input).get("url") or "").strip().rstrip("/")
    if not url:
        return None, "Set the url custom field to your vanDyke address."
    if not url.startswith(("http://", "https://")):
        return None, "The url custom field must start with http:// or https://."
    return url, None


def _get(url):
    request = urllib.request.Request(url, headers={"Accept": "application/json"})
    with urllib.request.urlopen(request, timeout=10) as response:
        return json.loads(response.read().decode("utf-8"))


def _date(value):
    if not value:
        return ""
    try:
        return datetime.fromisoformat(str(value).replace("Z", "+00:00")).strftime("%d %b %Y")
    except (TypeError, ValueError):
        return str(value)


def _photo(base, value):
    if not value:
        return ""
    if str(value).startswith(("http://", "https://")):
        return str(value)
    return base + "/" + str(value).lstrip("/")


def _entry(base, item):
    return {
        "word": item.get("word") or "",
        "meaning": item.get("meaning") or "",
        "date": _date(item.get("created_at")),
        "photo_url": _photo(base, item.get("photo_url")),
    }


def run(input):
    base, error = _base(input)
    if error:
        return {"error": error}
    try:
        stats = _get(base + "/api/v1/stats")
        page = _get(base + "/api/v1/entries?per_page=25&sort=created_at&order=desc")
    except Exception:
        return {"error": "Could not reach the vanDyke API. Check the url custom field."}

    items = [item for item in (page.get("items") or []) if isinstance(item, dict)]
    latest = [_entry(base, item) for item in items[:3]]
    photo = None
    for item in items:
        entry = _entry(base, item)
        if entry["photo_url"]:
            photo = entry
            break

    first = stats.get("first_entry_at")
    last = stats.get("last_entry_at")
    span = ""
    if first and last:
        try:
            start = datetime.fromisoformat(str(first).replace("Z", "+00:00"))
            end = datetime.fromisoformat(str(last).replace("Z", "+00:00"))
            span = str(max(1, end.year - start.year + 1))
        except (TypeError, ValueError):
            span = ""

    return {
        "total": stats.get("total_entries") or 0,
        "unique": stats.get("unique_words") or 0,
        "last30": stats.get("entries_last_30_days") or 0,
        "last_date": _date(last),
        "span_years": span,
        "entries": latest,
        "has_photo": photo is not None,
        "photo": photo or {},
    }


if __name__ == "__main__":
    raw = sys.stdin.read()
    try:
        payload = json.loads(raw) if raw.strip() else {}
    except json.JSONDecodeError:
        payload = {}
    print(json.dumps(run(payload)))
