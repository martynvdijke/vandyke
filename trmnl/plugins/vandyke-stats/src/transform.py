#!/usr/bin/env python3
"""vanDyke ledger statistics transform for TRMNL.

Reads the public JSON API exposed by a self-hosted vanDyke instance. Only the
Python standard library is used so the transform runs in the TRMNL sandbox.
"""

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
        parsed = datetime.fromisoformat(str(value).replace("Z", "+00:00"))
        return parsed.strftime("%d %b %Y")
    except (TypeError, ValueError):
        return str(value)


def run(input):
    base, error = _base(input)
    if error:
        return {"error": error}
    try:
        stats = _get(base + "/api/v1/stats")
    except Exception:
        return {"error": "Could not reach the vanDyke API. Check the url custom field."}

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
    }


if __name__ == "__main__":
    raw = sys.stdin.read()
    try:
        payload = json.loads(raw) if raw.strip() else {}
    except json.JSONDecodeError:
        payload = {}
    print(json.dumps(run(payload)))
