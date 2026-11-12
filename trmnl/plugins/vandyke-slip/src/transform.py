#!/usr/bin/env python3
"""Slip-of-the-day transform for TRMNL.

Reads the public `GET /api/v1/slip` endpoint and returns the day's misspelling,
its correction, the date, the attributed person and a photo URL when present.
"""

import json
import sys
import urllib.error
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


def run(input):
    base, error = _base(input)
    if error:
        return {"error": error}
    try:
        slip = _get(base + "/api/v1/slip")
    except urllib.error.HTTPError as exc:
        if exc.code == 404:
            return {"error": "The ledger has no entry for today."}
        return {"error": "Could not reach the vanDyke API. Check the url custom field."}
    except Exception:
        return {"error": "Could not reach the vanDyke API. Check the url custom field."}

    entry = slip.get("entry") or {}
    if not isinstance(entry, dict):
        return {"error": "The vanDyke API returned an unexpected slip."}

    photo_url = _photo(base, entry.get("photo_url"))
    return {
        "date": _date(slip.get("date")),
        "word": entry.get("word") or "",
        "meaning": entry.get("meaning") or "",
        "person": entry.get("person") or "",
        "photo_url": photo_url,
        "has_photo": bool(photo_url),
        "entry_id": entry.get("id") or 0,
    }


if __name__ == "__main__":
    raw = sys.stdin.read()
    try:
        payload = json.loads(raw) if raw.strip() else {}
    except json.JSONDecodeError:
        payload = {}
    print(json.dumps(run(payload)))
